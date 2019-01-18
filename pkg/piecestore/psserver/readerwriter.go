// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package psserver

import (
	"github.com/gogo/protobuf/proto"
	"github.com/zeebo/errs"

	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/utils"
)

// StreamWriterError is a type of error for failures in StreamWriter
var StreamWriterError = errs.Class("stream writer error")

// StreamWriter -- Struct for writing piece to server upload stream
type StreamWriter struct {
	server *Server
	stream pb.PieceStoreRoutes_RetrieveServer
}

// NewStreamWriter returns a new StreamWriter
func NewStreamWriter(s *Server, stream pb.PieceStoreRoutes_RetrieveServer) *StreamWriter {
	return &StreamWriter{server: s, stream: stream}
}

// Write -- Write method for piece upload to stream for Server.Retrieve
func (s *StreamWriter) Write(b []byte) (int, error) {
	// Write the buffer to the stream we opened earlier
	if err := s.stream.Send(&pb.PieceRetrievalStream{PieceSize: int64(len(b)), Content: b}); err != nil {
		return 0, err
	}

	return len(b), nil
}

// StreamReader is a struct for Retrieving data from server
type StreamReader struct {
	src                 *utils.ReaderSource
	bandwidthAllocation *pb.RenterBandwidthAllocation
	currentTotal        int64
	bandwidthRemaining  int64
	spaceRemaining      int64
	sofar               int64
}

// NewStreamReader returns a new StreamReader for Server.Store
func NewStreamReader(s *Server, stream pb.PieceStoreRoutes_StoreServer, bandwidthRemaining, spaceRemaining int64) *StreamReader {
	sr := &StreamReader{
		bandwidthRemaining: bandwidthRemaining,
		spaceRemaining:     spaceRemaining,
	}
	sr.src = utils.NewReaderSource(func() ([]byte, error) {

		recv, err := stream.Recv()
		if err != nil {
			return nil, err
		}

		pd := recv.GetPieceData()
		ba := recv.GetBandwidthAllocation()

		if ba != nil {
			if err = s.verifySignature(stream.Context(), ba); err != nil {
				return nil, err
			}

			deserializedData := &pb.RenterBandwidthAllocation_Data{}
			err = proto.Unmarshal(ba.GetData(), deserializedData)
			if err != nil {
				return nil, err
			}

			pbaData := &pb.PayerBandwidthAllocation_Data{}
			if err = proto.Unmarshal(deserializedData.GetPayerAllocation().GetData(), pbaData); err != nil {
				return nil, err
			}

			if err = s.verifyPayerAllocation(pbaData, "PUT"); err != nil {
				return nil, err
			}

			// Update bandwidthallocation to be stored
			if deserializedData.GetTotal() > sr.currentTotal {
				sr.bandwidthAllocation = ba
				sr.currentTotal = deserializedData.GetTotal()
			}
		}

		return pd.GetContent(), nil
	})

	return sr
}

// Read -- Read method for piece download from stream
func (s *StreamReader) Read(b []byte) (int, error) {
	if s.sofar >= s.bandwidthRemaining {
		return 0, StreamWriterError.New("out of bandwidth")
	}
	if s.sofar >= s.spaceRemaining {
		return 0, StreamWriterError.New("out of space")
	}

	n, err := s.src.Read(b)
	s.sofar += int64(n)
	if err != nil {
		return n, err
	}
	if s.sofar >= s.spaceRemaining {
		return n, StreamWriterError.New("out of space")
	}

	return n, nil
}
