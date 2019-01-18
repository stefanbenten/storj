// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package psserver

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/utils"
)

// OK - Success!
const OK = "OK"

// StoreError is a type of error for failures in Server.Store()
var StoreError = errs.Class("store error")

// Store incoming data using piecestore
func (s *Server) Store(reqStream pb.PieceStoreRoutes_StoreServer) (err error) {
	ctx := reqStream.Context()
	defer mon.Task()(&ctx)(&err)
	// Receive id/ttl
	recv, err := reqStream.Recv()
	if err != nil {
		return StoreError.Wrap(err)
	}
	if recv == nil {
		return StoreError.New("error receiving Piece metadata")
	}

	authorization := recv.GetAuthorization()
	if err := s.verifier(authorization); err != nil {
		return ServerError.Wrap(err)
	}

	pd := recv.GetPieceData()
	if pd == nil {
		return StoreError.New("PieceStore message is nil")
	}

	s.log.Debug("Storing", zap.String("Piece ID", fmt.Sprint(pd.GetId())))

	if pd.GetId() == "" {
		return StoreError.New("piece ID not specified")
	}

	id, err := getNamespacedPieceID([]byte(pd.GetId()), getNamespace(authorization))
	if err != nil {
		return err
	}
	total, err := s.storeData(ctx, reqStream, id)
	if err != nil {
		return err
	}

	if err = s.DB.AddTTL(id, pd.GetExpirationUnixSec(), total); err != nil {
		deleteErr := s.deleteByID(id)
		return StoreError.New("failed to write piece meta data to database: %v", utils.CombineErrors(err, deleteErr))
	}

	if err = s.DB.AddBandwidthUsed(total); err != nil {
		return StoreError.New("failed to write bandwidth info to database: %v", err)
	}
	s.log.Debug("Successfully stored", zap.String("Piece ID", fmt.Sprint(pd.GetId())))

	return reqStream.SendAndClose(&pb.PieceStoreSummary{Message: OK, TotalReceived: total})
}

func (s *Server) storeData(ctx context.Context, stream pb.PieceStoreRoutes_StoreServer, id string) (total int64, err error) {
	defer mon.Task()(&ctx)(&err)

	// Delete data if we error
	defer func() {
		if err != nil && err != io.EOF {
			if deleteErr := s.deleteByID(id); deleteErr != nil {
				s.log.Error("Failed on deleteByID in Store", zap.Error(deleteErr))
			}
		}
	}()

	// Initialize file for storing data
	storeFile, err := s.storage.Writer(id)
	if err != nil {
		return 0, err
	}

	defer utils.LogClose(storeFile)

	bwUsed, err := s.DB.GetTotalBandwidthBetween(getBeginningOfMonth(), time.Now())
	if err != nil {
		return 0, err
	}
	spaceUsed, err := s.DB.SumTTLSizes()
	if err != nil {
		return 0, err
	}
	bwLeft := s.totalBwAllocated - bwUsed
	spaceLeft := s.totalAllocated - spaceUsed
	reader := NewStreamReader(s, stream, bwLeft, spaceLeft)

	total, err = io.Copy(storeFile, reader)

	if err != nil && err != io.EOF {
		return 0, err
	}

	err = s.DB.WriteBandwidthAllocToDB(reader.bandwidthAllocation)

	return total, err
}
