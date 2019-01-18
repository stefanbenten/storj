// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package psserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/gogo/protobuf/proto"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/storj/internal/sync2"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/utils"
)

// RetrieveError is a type of error for failures in Server.Retrieve()
var RetrieveError = errs.Class("retrieve error")

// Retrieve -- Retrieve data from piecestore and send to client
func (s *Server) Retrieve(stream pb.PieceStoreRoutes_RetrieveServer) (err error) {
	ctx := stream.Context()
	defer mon.Task()(&ctx)(&err)

	// Receive Signature
	recv, err := stream.Recv()
	if err != nil {
		return RetrieveError.Wrap(err)
	}
	if recv == nil {
		return RetrieveError.New("error receiving piece data")
	}

	authorization := recv.GetAuthorization()
	if err := s.verifier(authorization); err != nil {
		return ServerError.Wrap(err)
	}

	pd := recv.GetPieceData()
	if pd == nil {
		return RetrieveError.New("PieceStore message is nil")
	}

	s.log.Debug("Retrieving",
		zap.String("Piece ID", fmt.Sprint(pd.GetId())),
		zap.Int64("Offset", pd.GetOffset()),
		zap.Int64("Size", pd.GetPieceSize()),
	)

	id, err := getNamespacedPieceID([]byte(pd.GetId()), getNamespace(authorization))
	if err != nil {
		return err
	}

	// Get path to data being retrieved
	path, err := s.storage.PiecePath(id)
	if err != nil {
		return err
	}

	// Verify that the path exists
	fileInfo, err := os.Stat(path)
	if err != nil {
		return RetrieveError.Wrap(err)
	}

	// Read the size specified
	totalToRead := pd.GetPieceSize()
	fileSize := fileInfo.Size()

	// Read the entire file if specified -1 but make sure we do it from the correct offset
	if pd.GetPieceSize() <= -1 || totalToRead+pd.GetOffset() > fileSize {
		totalToRead = fileSize - pd.GetOffset()
	}

	retrieved, allocated, err := s.retrieveData(ctx, stream, id, pd.GetOffset(), totalToRead)
	if err != nil {
		return err
	}

	s.log.Debug("Successfully retrieved",
		zap.String("Piece ID", fmt.Sprint(pd.GetId())),
		zap.Int64("Allocated", allocated),
		zap.Int64("Retrieved", retrieved),
	)
	return nil
}

func (s *Server) retrieveData(ctx context.Context, stream pb.PieceStoreRoutes_RetrieveServer, id string, offset, length int64) (retrieved, allocated int64, err error) {
	defer mon.Task()(&ctx)(&err)

	storeFile, err := s.storage.Reader(ctx, id, offset, length)
	if err != nil {
		return 0, 0, err
	}

	defer utils.LogClose(storeFile)

	writer := NewStreamWriter(s, stream)
	allocationTracking := sync2.NewThrottle()
	totalAllocated := int64(0)

	// Bandwidth Allocation recv loop
	go func() {
		var lastTotal int64
		var lastAllocation *pb.RenterBandwidthAllocation
		defer func() {
			if lastAllocation == nil {
				return
			}
			err := s.DB.WriteBandwidthAllocToDB(lastAllocation)
			if err != nil {
				// TODO: handle error properly
				s.log.Error("WriteBandwidthAllocToDB Error:", zap.Error(err))
			}
		}()

		for {
			recv, err := stream.Recv()
			if err != nil {
				allocationTracking.Fail(err)
				return
			}

			alloc := recv.GetBandwidthAllocation()
			allocData := &pb.RenterBandwidthAllocation_Data{}
			if err = proto.Unmarshal(alloc.GetData(), allocData); err != nil {
				allocationTracking.Fail(err)
				return
			}

			if err = s.verifySignature(ctx, alloc); err != nil {
				allocationTracking.Fail(err)
				return
			}

			if allocData.GetPayerAllocation() == nil {
				allocationTracking.Fail(StoreError.New("no payer bandwidth allocation"))
				return
			}

			pbaData := &pb.PayerBandwidthAllocation_Data{}
			if err = proto.Unmarshal(allocData.GetPayerAllocation().GetData(), pbaData); err != nil {
				allocationTracking.Fail(err)
				return
			}

			if err = s.verifyPayerAllocation(pbaData, "GET"); err != nil {
				allocationTracking.Fail(err)
				return
			}

			// TODO: break when lastTotal >= allocData.GetPayer_allocation().GetData().GetMax_size()

			if lastTotal > allocData.GetTotal() {
				allocationTracking.Fail(fmt.Errorf("got lower allocation was %v got %v", lastTotal, allocData.GetTotal()))
				return
			}

			atomic.StoreInt64(&totalAllocated, allocData.GetTotal())

			if err = allocationTracking.Produce(allocData.GetTotal() - lastTotal); err != nil {
				return
			}

			lastAllocation = alloc
			lastTotal = allocData.GetTotal()
		}
	}()

	// Data send loop
	messageSize := int64(32 << 10)
	used := int64(0)

	for used < length {
		nextMessageSize, err := allocationTracking.ConsumeOrWait(messageSize)
		if err != nil {
			allocationTracking.Fail(err)
			break
		}

		used += nextMessageSize
		n, err := io.CopyN(writer, storeFile, nextMessageSize)
		// correct errors when needed
		if n != nextMessageSize {
			if pErr := allocationTracking.Produce(nextMessageSize - n); pErr != nil {
				break
			}
			used -= nextMessageSize - n
		}
		// break on error
		if err != nil {
			allocationTracking.Fail(err)
			break
		}
	}

	// write to bandwidth usage table
	if err = s.DB.AddBandwidthUsed(used); err != nil {
		return retrieved, allocated, StoreError.New("failed to write bandwidth info to database: %v", err)
	}

	// TODO: handle errors
	// _ = stream.Close()

	return used, atomic.LoadInt64(&totalAllocated), allocationTracking.Err()
}
