// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package pointerdb

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/pkg/auth"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/peertls"
	pointerdbAuth "storj.io/storj/pkg/pointerdb/auth"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/storage/meta"
	"storj.io/storj/storage"
)

var (
	mon          = monkit.Package()
	segmentError = errs.Class("segment error")
)

// Server implements the network state RPC service
type Server struct {
	DB       storage.KeyValueStore
	logger   *zap.Logger
	config   Config
	cache    *overlay.Cache
	identity *provider.FullIdentity
}

// NewServer creates instance of Server
func NewServer(db storage.KeyValueStore, cache *overlay.Cache, logger *zap.Logger, c Config, identity *provider.FullIdentity) *Server {
	return &Server{
		DB:       db,
		logger:   logger,
		config:   c,
		cache:    cache,
		identity: identity,
	}
}

func (s *Server) validateAuth(ctx context.Context) error {
	APIKey, ok := auth.GetAPIKey(ctx)
	if !ok || !pointerdbAuth.ValidateAPIKey(string(APIKey)) {
		s.logger.Error("unauthorized request: ", zap.Error(status.Errorf(codes.Unauthenticated, "Invalid API credential")))
		return status.Errorf(codes.Unauthenticated, "Invalid API credential")
	}
	return nil
}

func (s *Server) validateSegment(req *pb.PutRequest) error {
	min := s.config.MinRemoteSegmentSize
	remote := req.GetPointer().Remote
	remoteSize := req.GetPointer().GetSegmentSize()

	if remote != nil && remoteSize < int64(min) {
		return segmentError.New("remote segment size %d less than minimum allowed %d", remoteSize, min)
	}

	max := s.config.MaxInlineSegmentSize.Int()
	inlineSize := len(req.GetPointer().InlineSegment)

	if inlineSize > max {
		return segmentError.New("inline segment size %d greater than maximum allowed %d", inlineSize, max)
	}

	return nil
}

// Put formats and hands off a key/value (path/pointer) to be saved to boltdb
func (s *Server) Put(ctx context.Context, req *pb.PutRequest) (resp *pb.PutResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	err = s.validateSegment(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	if err = s.validateAuth(ctx); err != nil {
		return nil, err
	}

	// Update the pointer with the creation date
	req.GetPointer().CreationDate = ptypes.TimestampNow()

	pointerBytes, err := proto.Marshal(req.GetPointer())
	if err != nil {
		s.logger.Error("err marshaling pointer", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	// TODO(kaloyan): make sure that we know we are overwriting the pointer!
	// In such case we should delete the pieces of the old segment if it was
	// a remote one.
	if err = s.DB.Put([]byte(req.GetPath()), pointerBytes); err != nil {
		s.logger.Error("err putting pointer", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &pb.PutResponse{}, nil
}

// Get formats and hands off a file path to get from boltdb
func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (resp *pb.GetResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = s.validateAuth(ctx); err != nil {
		return nil, err
	}

	pointerBytes, err := s.DB.Get([]byte(req.GetPath()))
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, status.Errorf(codes.NotFound, err.Error())
		}
		s.logger.Error("err getting pointer", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	pointer := &pb.Pointer{}
	err = proto.Unmarshal(pointerBytes, pointer)
	if err != nil {
		s.logger.Error("Error unmarshaling pointer")
		return nil, err
	}

	pba, err := s.PayerBandwidthAllocation(ctx, &pb.PayerBandwidthAllocationRequest{Action: pb.PayerBandwidthAllocation_GET})
	if err != nil {
		s.logger.Error("err getting payer bandwidth allocation", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	authorization, err := s.getSignedMessage()
	if err != nil {
		s.logger.Error("err getting signed message", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	nodes := []*pb.Node{}

	var r = &pb.GetResponse{
		Pointer:       pointer,
		Nodes:         nil,
		Pba:           pba.GetPba(),
		Authorization: authorization,
	}

	if !s.config.Overlay || pointer.Remote == nil {
		return r, nil
	}

	for _, piece := range pointer.Remote.RemotePieces {
		node, err := s.cache.Get(ctx, piece.NodeId)
		if err != nil {
			s.logger.Error("Error getting node from cache")
		}
		nodes = append(nodes, node)
	}

	for _, v := range nodes {
		v.Type.DPanicOnInvalid("pdb server Get")
	}
	r = &pb.GetResponse{
		Pointer:       pointer,
		Nodes:         nodes,
		Pba:           pba.GetPba(),
		Authorization: authorization,
	}

	return r, nil
}

// List returns all Path keys in the Pointers bucket
func (s *Server) List(ctx context.Context, req *pb.ListRequest) (resp *pb.ListResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = s.validateAuth(ctx); err != nil {
		return nil, err
	}

	var prefix storage.Key
	if req.Prefix != "" {
		prefix = storage.Key(req.Prefix)
		if prefix[len(prefix)-1] != storage.Delimiter {
			prefix = append(prefix, storage.Delimiter)
		}
	}

	rawItems, more, err := storage.ListV2(s.DB, storage.ListOptions{
		Prefix:       prefix, //storage.Key(req.Prefix),
		StartAfter:   storage.Key(req.StartAfter),
		EndBefore:    storage.Key(req.EndBefore),
		Recursive:    req.Recursive,
		Limit:        int(req.Limit),
		IncludeValue: req.MetaFlags != meta.None,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ListV2: %v", err)
	}

	var items []*pb.ListResponse_Item
	for _, rawItem := range rawItems {
		items = append(items, s.createListItem(rawItem, req.MetaFlags))
	}

	return &pb.ListResponse{Items: items, More: more}, nil
}

// createListItem creates a new list item with the given path. It also adds
// the metadata according to the given metaFlags.
func (s *Server) createListItem(rawItem storage.ListItem, metaFlags uint32) *pb.ListResponse_Item {
	item := &pb.ListResponse_Item{
		Path:     rawItem.Key.String(),
		IsPrefix: rawItem.IsPrefix,
	}
	if item.IsPrefix {
		return item
	}

	err := s.setMetadata(item, rawItem.Value, metaFlags)
	if err != nil {
		s.logger.Warn("err retrieving metadata", zap.Error(err))
	}
	return item
}

// getMetadata adds the metadata to the given item pointer according to the
// given metaFlags
func (s *Server) setMetadata(item *pb.ListResponse_Item, data []byte, metaFlags uint32) (err error) {
	if metaFlags == meta.None || len(data) == 0 {
		return nil
	}

	pr := &pb.Pointer{}
	err = proto.Unmarshal(data, pr)
	if err != nil {
		return err
	}

	// Start with an empty pointer to and add only what's requested in
	// metaFlags to safe to transfer payload
	item.Pointer = &pb.Pointer{}
	if metaFlags&meta.Modified != 0 {
		item.Pointer.CreationDate = pr.GetCreationDate()
	}
	if metaFlags&meta.Expiration != 0 {
		item.Pointer.ExpirationDate = pr.GetExpirationDate()
	}
	if metaFlags&meta.Size != 0 {
		item.Pointer.SegmentSize = pr.GetSegmentSize()
	}
	if metaFlags&meta.UserDefined != 0 {
		item.Pointer.Metadata = pr.GetMetadata()
	}

	return nil
}

// Delete formats and hands off a file path to delete from boltdb
func (s *Server) Delete(ctx context.Context, req *pb.DeleteRequest) (resp *pb.DeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = s.validateAuth(ctx); err != nil {
		return nil, err
	}

	err = s.DB.Delete([]byte(req.GetPath()))
	if err != nil {
		s.logger.Error("err deleting path and pointer", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &pb.DeleteResponse{}, nil
}

// Iterate iterates over items based on IterateRequest
func (s *Server) Iterate(ctx context.Context, req *pb.IterateRequest, f func(it storage.Iterator) error) (err error) {
	defer mon.Task()(&ctx)(&err)

	if err = s.validateAuth(ctx); err != nil {
		return err
	}

	opts := storage.IterateOptions{
		Prefix:  storage.Key(req.Prefix),
		First:   storage.Key(req.First),
		Recurse: req.Recurse,
		Reverse: req.Reverse,
	}
	return s.DB.Iterate(opts, f)
}

// PayerBandwidthAllocation returns PayerBandwidthAllocation struct, signed and with given action type
func (s *Server) PayerBandwidthAllocation(ctx context.Context, req *pb.PayerBandwidthAllocationRequest) (pba *pb.PayerBandwidthAllocationResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = s.validateAuth(ctx); err != nil {
		return nil, err
	}

	payer := s.identity.ID

	// TODO(michal) should be replaced with renter id when available
	// retrieve the public key
	pi, err := provider.PeerIdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}

	pk, ok := pi.Leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, peertls.ErrUnsupportedKey.New("%T", pi.Leaf.PublicKey)
	}

	pubbytes, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		s.logger.Error("Can't Marshal Public Key for PayerBandwidthAllocation: %+v", zap.Error(err))
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	serialNum, err := uuid.New()
	if err != nil {
		return nil, err
	}

	created := time.Now().Unix()

	// convert ttl from days to seconds
	ttl := s.config.BwExpiration
	ttl *= 86400

	pbad := &pb.PayerBandwidthAllocation_Data{
		SatelliteId:       payer,
		UplinkId:          pi.ID,
		CreatedUnixSec:    created,
		ExpirationUnixSec: created + int64(ttl),
		Action:            req.GetAction(),
		SerialNumber:      serialNum.String(),
		PubKey:            pubbytes,
	}

	data, err := proto.Marshal(pbad)
	if err != nil {
		return nil, err
	}
	signature, err := auth.GenerateSignature(data, s.identity)
	if err != nil {
		return nil, err
	}
	return &pb.PayerBandwidthAllocationResponse{Pba: &pb.PayerBandwidthAllocation{Signature: signature, Data: data}}, nil
}

func (s *Server) getSignedMessage() (*pb.SignedMessage, error) {
	signature, err := auth.GenerateSignature(s.identity.ID.Bytes(), s.identity)
	if err != nil {
		return nil, err
	}

	return auth.NewSignedMessage(signature, s.identity)
}
