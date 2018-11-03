// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package server

import (
	"context"
	"io"
	"log"

	"github.com/zeebo/errs"

	"storj.io/storj/pkg/pb"
	pstore "storj.io/storj/pkg/piecestore"
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
		return StoreError.New("Error receiving Piece metadata")
	}

	authorization := recv.GetAuthorization()
	if err := s.verifier(authorization); err != nil {
		return err
	}

	pd := recv.GetPiecedata()
	if pd == nil {
		return StoreError.New("PieceStore message is nil")
	}

	log.Printf("Storing %s...", pd.GetId())

	if pd.GetId() == "" {
		return StoreError.New("Piece ID not specified")
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
	log.Printf("Successfully stored %s.", pd.GetId())

	return reqStream.SendAndClose(&pb.PieceStoreSummary{Message: OK, TotalReceived: total})
}

func (s *Server) storeData(ctx context.Context, stream pb.PieceStoreRoutes_StoreServer, id string) (total int64, err error) {
	defer mon.Task()(&ctx)(&err)

	// Delete data if we error
	defer func() {
		if err != nil && err != io.EOF {
			if deleteErr := s.deleteByID(id); deleteErr != nil {
				log.Printf("Failed on deleteByID in Store: %s", deleteErr.Error())
			}
		}
	}()

	// Initialize file for storing data
	storeFile, err := pstore.StoreWriter(id, s.DataDir)
	if err != nil {
		return 0, err
	}

	defer utils.LogClose(storeFile)

	reader := NewStreamReader(s, stream)

	defer func() {
		baWriteErr := s.DB.WriteBandwidthAllocToDB(reader.bandwidthAllocation)
		if baWriteErr != nil {
			log.Printf("WriteBandwidthAllocToDB Error: %s\n", baWriteErr.Error())
		}
	}()

	total, err = io.Copy(storeFile, reader)

	if err != nil && err != io.EOF {
		return 0, err
	}

	return total, nil
}
