// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package audit

import (
	"bytes"
	"context"
	"io"

	"github.com/vivint/infectious"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/piecestore/psclient"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/pkg/transport"
	"storj.io/storj/pkg/utils"
)

var mon = monkit.Package()

type share struct {
	Error       error
	PieceNumber int
	Data        []byte
}

// Verifier helps verify the correctness of a given stripe
type Verifier struct {
	downloader downloader
}

type downloader interface {
	DownloadShares(ctx context.Context, pointer *pb.Pointer, stripeIndex int, pba *pb.PayerBandwidthAllocation,
		authorization *pb.SignedMessage) (shares map[int]share, nodes map[int]*pb.Node, err error)
}

// defaultDownloader downloads shares from networked storage nodes
type defaultDownloader struct {
	transport transport.Client
	overlay   overlay.Client
	identity  provider.FullIdentity
	reporter
}

// newDefaultDownloader creates a defaultDownloader
func newDefaultDownloader(transport transport.Client, overlay overlay.Client, id provider.FullIdentity) *defaultDownloader {
	return &defaultDownloader{transport: transport, overlay: overlay, identity: id}
}

// NewVerifier creates a Verifier
func NewVerifier(transport transport.Client, overlay overlay.Client, id provider.FullIdentity) *Verifier {
	return &Verifier{downloader: newDefaultDownloader(transport, overlay, id)}
}

// getShare use piece store clients to download shares from a given node
func (d *defaultDownloader) getShare(ctx context.Context, stripeIndex, shareSize, pieceNumber int,
	id psclient.PieceID, pieceSize int64, fromNode *pb.Node, pba *pb.PayerBandwidthAllocation, authorization *pb.SignedMessage) (s share, err error) {
	defer mon.Task()(&ctx)(&err)

	if fromNode == nil {
		// TODO(moby) perhaps we should not penalize this node's reputation if it is not returned by the overlay
		return s, Error.New("no node returned from overlay for piece %s", id.String())
	}
	fromNode.Type.DPanicOnInvalid("audit getShare")
	ps, err := psclient.NewPSClient(ctx, d.transport, fromNode, 0)
	if err != nil {
		return s, err
	}

	derivedPieceID, err := id.Derive(fromNode.Id.Bytes())
	if err != nil {
		return s, err
	}

	rr, err := ps.Get(ctx, derivedPieceID, pieceSize, pba, authorization)
	if err != nil {
		return s, err
	}

	offset := shareSize * stripeIndex

	rc, err := rr.Range(ctx, int64(offset), int64(shareSize))
	if err != nil {
		return s, err
	}
	defer utils.LogClose(rc)

	buf := make([]byte, shareSize)
	_, err = io.ReadFull(rc, buf)
	if err != nil {
		return s, err
	}

	s = share{
		Error:       nil,
		PieceNumber: pieceNumber,
		Data:        buf,
	}
	return s, nil
}

// Download Shares downloads shares from the nodes where remote pieces are located
func (d *defaultDownloader) DownloadShares(ctx context.Context, pointer *pb.Pointer,
	stripeIndex int, pba *pb.PayerBandwidthAllocation, authorization *pb.SignedMessage) (shares map[int]share, nodes map[int]*pb.Node, err error) {
	defer mon.Task()(&ctx)(&err)

	var nodeIds storj.NodeIDList
	pieces := pointer.Remote.GetRemotePieces()

	for _, p := range pieces {
		nodeIds = append(nodeIds, p.NodeId)
	}

	// TODO(moby) nodeSlice will not include offline nodes, so overlay should update uptime for these nodes
	nodeSlice, err := d.overlay.BulkLookup(ctx, nodeIds)
	if err != nil {
		return nil, nodes, err
	}

	shares = make(map[int]share, len(nodeSlice))
	nodes = make(map[int]*pb.Node, len(nodeSlice))

	shareSize := int(pointer.Remote.Redundancy.GetErasureShareSize())
	pieceID := psclient.PieceID(pointer.Remote.GetPieceId())

	// this downloads shares from nodes at the given stripe index
	for i, node := range nodeSlice {
		paddedSize := calcPadded(pointer.GetSegmentSize(), shareSize)
		pieceSize := paddedSize / int64(pointer.Remote.Redundancy.GetMinReq())

		s, err := d.getShare(ctx, stripeIndex, shareSize, int(pieces[i].PieceNum), pieceID, pieceSize, node, pba, authorization)
		if err != nil {
			s = share{
				Error:       err,
				PieceNumber: int(pieces[i].PieceNum),
				Data:        nil,
			}
		}

		shares[s.PieceNumber] = s
		nodes[s.PieceNumber] = node
	}

	return shares, nodes, nil
}

func makeCopies(ctx context.Context, originals map[int]share) (copies []infectious.Share, err error) {
	defer mon.Task()(&ctx)(&err)
	copies = make([]infectious.Share, 0, len(originals))
	for _, original := range originals {
		if original.Error != nil {
			continue
		}
		copies = append(copies, infectious.Share{
			Data:   append([]byte{}, original.Data...),
			Number: original.PieceNumber})
	}
	return copies, nil
}

// auditShares takes the downloaded shares and uses infectious's Correct function to check that they
// haven't been altered. auditShares returns a slice containing the piece numbers of altered shares.
func auditShares(ctx context.Context, required, total int, originals map[int]share) (pieceNums []int, err error) {
	defer mon.Task()(&ctx)(&err)
	f, err := infectious.NewFEC(required, total)
	if err != nil {
		return nil, err
	}

	copies, err := makeCopies(ctx, originals)
	if err != nil {
		return nil, err
	}

	err = f.Correct(copies)
	if err != nil {
		return nil, err
	}
	for _, share := range copies {
		if !bytes.Equal(originals[share.Number].Data, share.Data) {
			pieceNums = append(pieceNums, share.Number)
		}
	}
	return pieceNums, nil
}

func calcPadded(size int64, blockSize int) int64 {
	mod := size % int64(blockSize)
	if mod == 0 {
		return size
	}
	return size + int64(blockSize) - mod
}

// verify downloads shares then verifies the data correctness at the given stripe
func (verifier *Verifier) verify(ctx context.Context, stripe *Stripe) (verifiedNodes *RecordAuditsInfo, err error) {
	defer mon.Task()(&ctx)(&err)

	shares, nodes, err := verifier.downloader.DownloadShares(ctx, stripe.Segment, stripe.Index, stripe.PBA, stripe.Authorization)
	if err != nil {
		return nil, err
	}

	var offlineNodes storj.NodeIDList
	for pieceNum := range shares {
		if shares[pieceNum].Error != nil {
			offlineNodes = append(offlineNodes, nodes[pieceNum].Id)
		}
	}

	pointer := stripe.Segment
	required := int(pointer.Remote.Redundancy.GetMinReq())
	total := int(pointer.Remote.Redundancy.GetTotal())
	pieceNums, err := auditShares(ctx, required, total, shares)
	if err != nil {
		return nil, err
	}

	var failedNodes storj.NodeIDList
	for _, pieceNum := range pieceNums {
		failedNodes = append(failedNodes, nodes[pieceNum].Id)
	}

	successNodes := getSuccessNodes(ctx, nodes, failedNodes, offlineNodes)

	return &RecordAuditsInfo{
		SuccessNodeIDs: successNodes,
		FailNodeIDs:    failedNodes,
		OfflineNodeIDs: offlineNodes,
	}, nil
}

// getSuccessNodes uses the failed nodes and offline nodes arrays to determine which nodes passed the audit
func getSuccessNodes(ctx context.Context, nodes map[int]*pb.Node, failedNodes, offlineNodes storj.NodeIDList) (successNodes storj.NodeIDList) {
	fails := make(map[storj.NodeID]bool)
	for _, fail := range failedNodes {
		fails[fail] = true
	}
	for _, offline := range offlineNodes {
		fails[offline] = true
	}

	for _, node := range nodes {
		if !fails[node.Id] {
			successNodes = append(successNodes, node.Id)
		}
	}
	return successNodes
}
