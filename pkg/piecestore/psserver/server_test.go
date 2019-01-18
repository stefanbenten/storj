// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package psserver

import (
	"crypto"
	"crypto/ecdsa"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gtank/cryptopasta"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/zeebo/errs"
	"go.uber.org/zap/zaptest"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testidentity"
	"storj.io/storj/internal/teststorj"
	"storj.io/storj/pkg/pb"
	pstore "storj.io/storj/pkg/piecestore"
	"storj.io/storj/pkg/piecestore/psserver/psdb"
	"storj.io/storj/pkg/storj"
)

func (TS *TestServer) writeFile(pieceID string) error {
	file, err := TS.s.storage.Writer(pieceID)
	if err != nil {
		return err
	}

	_, err = file.Write([]byte("xyzwq"))
	return errs.Combine(err, file.Close())

}

func TestPiece(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	TS := NewTestServer(t)
	defer TS.Stop()

	if err := TS.writeFile("11111111111111111111"); err != nil {
		t.Errorf("Error: %v\nCould not create test piece", err)
		return
	}

	defer func() { _ = TS.s.storage.Delete("11111111111111111111") }()

	// set up test cases
	tests := []struct {
		id         string
		size       int64
		expiration int64
		err        string
	}{
		{ // should successfully retrieve piece meta-data
			id:         "11111111111111111111",
			size:       5,
			expiration: 9999999999,
			err:        "",
		},
		{ // server should err with invalid id
			id:         "123",
			size:       5,
			expiration: 9999999999,
			err:        "rpc error: code = Unknown desc = piecestore error: invalid id length",
		},
		{ // server should err with nonexistent file
			id:         "22222222222222222222",
			size:       5,
			expiration: 9999999999,
			err: fmt.Sprintf("rpc error: code = Unknown desc = stat %s: no such file or directory", func() string {
				path, _ := TS.s.storage.PiecePath("22222222222222222222")
				return path
			}()),
		},
		{ // server should err with invalid TTL
			id:         "22222222222222222222;DELETE*FROM TTL;;;;",
			size:       5,
			expiration: 9999999999,
			err:        "rpc error: code = Unknown desc = PSServer error: invalid ID",
		},
	}

	for _, tt := range tests {
		t.Run("should return expected PieceSummary values", func(t *testing.T) {
			assert := assert.New(t)

			// simulate piece TTL entry
			_, err := TS.s.DB.DB.Exec(fmt.Sprintf(`INSERT INTO ttl (id, created, expires) VALUES ("%s", "%d", "%d")`, tt.id, 1234567890, tt.expiration))
			assert.NoError(err)

			defer func() {
				_, err := TS.s.DB.DB.Exec(fmt.Sprintf(`DELETE FROM ttl WHERE id="%s"`, tt.id))
				assert.NoError(err)
			}()

			req := &pb.PieceId{Id: tt.id}
			resp, err := TS.c.Piece(ctx, req)

			if tt.err != "" {
				assert.NotNil(err)
				if runtime.GOOS == "windows" && strings.Contains(tt.err, "no such file or directory") {
					//TODO (windows): ignoring for windows due to different underlying error
					return
				}
				assert.Equal(tt.err, err.Error())
				return
			}

			assert.NoError(err)

			assert.Equal(tt.id, resp.GetId())
			assert.Equal(tt.size, resp.GetPieceSize())
			assert.Equal(tt.expiration, resp.GetExpirationUnixSec())
		})
	}
}

func TestRetrieve(t *testing.T) {
	t.Skip("broken test")

	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	TS := NewTestServer(t)
	defer TS.Stop()

	if err := TS.writeFile("11111111111111111111"); err != nil {
		t.Errorf("Error: %v\nCould not create test piece", err)
		return
	}

	defer func() { _ = TS.s.storage.Delete("11111111111111111111") }()

	// set up test cases
	tests := []struct {
		id        string
		reqSize   int64
		respSize  int64
		allocSize int64
		offset    int64
		content   []byte
		err       string
	}{
		{ // should successfully retrieve data
			id:        "11111111111111111111",
			reqSize:   5,
			respSize:  5,
			allocSize: 5,
			offset:    0,
			content:   []byte("xyzwq"),
			err:       "",
		},
		{ // should successfully retrieve data in customizeable increments
			id:        "11111111111111111111",
			reqSize:   5,
			respSize:  5,
			allocSize: 2,
			offset:    0,
			content:   []byte("xyzwq"),
			err:       "",
		},
		{ // should successfully retrieve data with lower allocations
			id:        "11111111111111111111",
			reqSize:   5,
			respSize:  3,
			allocSize: 3,
			offset:    0,
			content:   []byte("but"),
			err:       "",
		},
		{ // should successfully retrieve data
			id:        "11111111111111111111",
			reqSize:   -1,
			respSize:  5,
			allocSize: 5,
			offset:    0,
			content:   []byte("xyzwq"),
			err:       "",
		},
		{ // server should err with invalid id
			id:        "123",
			reqSize:   5,
			respSize:  5,
			allocSize: 5,
			offset:    0,
			content:   []byte("xyzwq"),
			err:       "rpc error: code = Unknown desc = piecestore error: invalid id length",
		},
		{ // server should err with nonexistent file
			id:        "22222222222222222222",
			reqSize:   5,
			respSize:  5,
			allocSize: 5,
			offset:    0,
			content:   []byte("xyzwq"),
			err: fmt.Sprintf("rpc error: code = Unknown desc = retrieve error: stat %s: no such file or directory", func() string {
				path, _ := TS.s.storage.PiecePath("22222222222222222222")
				return path
			}()),
		},
		{ // server should return expected content and respSize with offset and excess reqSize
			id:        "11111111111111111111",
			reqSize:   5,
			respSize:  4,
			allocSize: 5,
			offset:    1,
			content:   []byte("utts"),
			err:       "",
		},
		{ // server should return expected content with reduced reqSize
			id:        "11111111111111111111",
			reqSize:   4,
			respSize:  4,
			allocSize: 5,
			offset:    0,
			content:   []byte("butt"),
			err:       "",
		},
	}

	for _, tt := range tests {
		t.Run("should return expected PieceRetrievalStream values", func(t *testing.T) {
			assert := assert.New(t)
			stream, err := TS.c.Retrieve(ctx)
			assert.NoError(err)

			// send piece database
			err = stream.Send(&pb.PieceRetrieval{PieceData: &pb.PieceRetrieval_PieceData{Id: tt.id, PieceSize: tt.reqSize, Offset: tt.offset}})
			assert.NoError(err)

			totalAllocated := int64(0)
			var data string
			var totalRetrieved = int64(0)
			var resp *pb.PieceRetrievalStream
			for totalAllocated < tt.respSize {
				// Send bandwidth bandwidthAllocation
				totalAllocated += tt.allocSize

				ba := pb.RenterBandwidthAllocation{
					Data: serializeData(&pb.RenterBandwidthAllocation_Data{
						PayerAllocation: &pb.PayerBandwidthAllocation{},
						Total:           totalAllocated,
					}),
				}

				s, err := cryptopasta.Sign(ba.Data, TS.k.(*ecdsa.PrivateKey))
				assert.NoError(err)
				ba.Signature = s

				err = stream.Send(
					&pb.PieceRetrieval{
						BandwidthAllocation: &ba,
					},
				)
				assert.NoError(err)

				resp, err = stream.Recv()
				if tt.err != "" {
					assert.NotNil(err)
					if runtime.GOOS == "windows" && strings.Contains(tt.err, "no such file or directory") {
						//TODO (windows): ignoring for windows due to different underlying error
						return
					}
					assert.Equal(tt.err, err.Error())
					return
				}

				data = fmt.Sprintf("%s%s", data, string(resp.GetContent()))
				totalRetrieved += resp.GetPieceSize()
			}

			assert.NoError(err)
			assert.NotNil(resp)
			if resp != nil {
				assert.Equal(tt.respSize, totalRetrieved)
				assert.Equal(string(tt.content), data)
			}
		})
	}
}

func TestStore(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	TS := NewTestServer(t)
	defer TS.Stop()

	db := TS.s.DB.DB

	tests := []struct {
		id            string
		ttl           int64
		content       []byte
		message       string
		totalReceived int64
		err           string
	}{
		{ // should successfully store data
			id:            "99999999999999999999",
			ttl:           9999999999,
			content:       []byte("xyzwq"),
			message:       "OK",
			totalReceived: 5,
			err:           "",
		},
		{ // should err with invalid id length
			id:            "butts",
			ttl:           9999999999,
			content:       []byte("xyzwq"),
			message:       "",
			totalReceived: 0,
			err:           "rpc error: code = Unknown desc = piecestore error: invalid id length",
		},
		{ // should err with piece ID not specified
			id:            "",
			ttl:           9999999999,
			content:       []byte("xyzwq"),
			message:       "",
			totalReceived: 0,
			err:           "rpc error: code = Unknown desc = store error: piece ID not specified",
		},
	}

	for _, tt := range tests {
		t.Run("should return expected PieceStoreSummary values", func(t *testing.T) {
			assert := assert.New(t)
			stream, err := TS.c.Store(ctx)
			assert.NoError(err)

			// Write the buffer to the stream we opened earlier
			err = stream.Send(&pb.PieceStore{PieceData: &pb.PieceStore_PieceData{Id: tt.id, ExpirationUnixSec: tt.ttl}})
			assert.NoError(err)

			pbad := &pb.PayerBandwidthAllocation_Data{
				SatelliteId: teststorj.NodeIDFromString("satelliteid"),
				UplinkId:    teststorj.NodeIDFromString("uplinkid"),
				Action:      pb.PayerBandwidthAllocation_PUT,
			}
			pbaData, err := proto.Marshal(pbad)
			assert.NoError(err)
			pba := &pb.PayerBandwidthAllocation{Data: pbaData}
			// Send Bandwidth Allocation Data
			msg := &pb.PieceStore{
				PieceData: &pb.PieceStore_PieceData{Content: tt.content},
				BandwidthAllocation: &pb.RenterBandwidthAllocation{
					Data: serializeData(&pb.RenterBandwidthAllocation_Data{
						PayerAllocation: pba,
						Total:           int64(len(tt.content)),
					}),
				},
			}

			s, err := cryptopasta.Sign(msg.BandwidthAllocation.Data, TS.k.(*ecdsa.PrivateKey))
			assert.NoError(err)
			msg.BandwidthAllocation.Signature = s

			// Write the buffer to the stream we opened earlier
			err = stream.Send(msg)
			if err != io.EOF && err != nil {
				assert.NoError(err)
			}

			resp, err := stream.CloseAndRecv()
			if tt.err != "" {
				assert.NotNil(err)
				assert.Equal(tt.err, err.Error())
				return
			}

			assert.NoError(err)

			defer func() {
				_, err := db.Exec(fmt.Sprintf(`DELETE FROM ttl WHERE id="%s"`, tt.id))
				assert.NoError(err)
			}()

			// check db to make sure agreement and signature were stored correctly
			rows, err := db.Query(`SELECT agreement, signature FROM bandwidth_agreements`)
			assert.NoError(err)

			defer func() { assert.NoError(rows.Close()) }()
			for rows.Next() {
				var (
					agreement []byte
					signature []byte
				)

				err = rows.Scan(&agreement, &signature)
				assert.NoError(err)

				decoded := &pb.RenterBandwidthAllocation_Data{}

				err = proto.Unmarshal(agreement, decoded)
				assert.NoError(err)
				assert.Equal(msg.BandwidthAllocation.GetSignature(), signature)
				assert.True(proto.Equal(pba, decoded.GetPayerAllocation()))
				assert.Equal(int64(len(tt.content)), decoded.GetTotal())

			}
			err = rows.Err()
			assert.NoError(err)

			assert.Equal(tt.message, resp.Message)
			assert.Equal(tt.totalReceived, resp.TotalReceived)
		})
	}
}

func TestPbaValidation(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	TS := NewTestServer(t)
	defer TS.Stop()

	tests := []struct {
		satelliteID storj.NodeID
		uplinkID    storj.NodeID
		action      pb.PayerBandwidthAllocation_Action
		err         string
	}{
		{ // missing satellite id
			satelliteID: storj.NodeID{},
			uplinkID:    teststorj.NodeIDFromString("uplinkid"),
			action:      pb.PayerBandwidthAllocation_PUT,
			err:         "rpc error: code = Unknown desc = store error: payer bandwidth allocation: missing satellite id",
		},
		{ // missing uplink id
			satelliteID: teststorj.NodeIDFromString("satelliteid"),
			uplinkID:    storj.NodeID{},
			action:      pb.PayerBandwidthAllocation_PUT,
			err:         "rpc error: code = Unknown desc = store error: payer bandwidth allocation: missing uplink id",
		},
		{ // wrong action type
			satelliteID: teststorj.NodeIDFromString("satelliteid"),
			uplinkID:    teststorj.NodeIDFromString("uplinkid"),
			action:      pb.PayerBandwidthAllocation_GET,
			err:         "rpc error: code = Unknown desc = store error: payer bandwidth allocation: invalid action GET",
		},
	}

	for _, tt := range tests {
		t.Run("should validate payer bandwidth allocation struct", func(t *testing.T) {
			assert := assert.New(t)
			stream, err := TS.c.Store(ctx)
			assert.NoError(err)

			// Write the buffer to the stream we opened earlier
			err = stream.Send(&pb.PieceStore{PieceData: &pb.PieceStore_PieceData{Id: "99999999999999999999", ExpirationUnixSec: 9999999999}})
			assert.NoError(err)

			pbad := &pb.PayerBandwidthAllocation_Data{
				SatelliteId: tt.satelliteID,
				UplinkId:    tt.uplinkID,
				Action:      tt.action,
			}
			pbaData, err := proto.Marshal(pbad)
			assert.NoError(err)
			pba := &pb.PayerBandwidthAllocation{Data: pbaData}
			// Send Bandwidth Allocation Data
			content := []byte("content")
			msg := &pb.PieceStore{
				PieceData: &pb.PieceStore_PieceData{Content: content},
				BandwidthAllocation: &pb.RenterBandwidthAllocation{
					Data: serializeData(&pb.RenterBandwidthAllocation_Data{
						PayerAllocation: pba,
						Total:           int64(len(content)),
					}),
				},
			}

			s, err := cryptopasta.Sign(msg.BandwidthAllocation.Data, TS.k.(*ecdsa.PrivateKey))
			assert.NoError(err)
			msg.BandwidthAllocation.Signature = s

			// Write the buffer to the stream we opened earlier
			err = stream.Send(msg)
			if err != io.EOF && err != nil {
				assert.NoError(err)
			}

			_, err = stream.CloseAndRecv()
			if err != nil {
				//assert.NotNil(err)
				assert.Equal(tt.err, err.Error())
				return
			}
		})
	}
}

func TestDelete(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	TS := NewTestServer(t)
	defer TS.Stop()

	db := TS.s.DB.DB

	// set up test cases
	tests := []struct {
		id      string
		message string
		err     string
	}{
		{ // should successfully delete data
			id:      "11111111111111111111",
			message: "OK",
			err:     "",
		},
		{ // should err with invalid id length
			id:      "123",
			message: "rpc error: code = Unknown desc = piecestore error: invalid id length",
			err:     "rpc error: code = Unknown desc = piecestore error: invalid id length",
		},
		{ // should return OK with nonexistent file
			id:      "22222222222222222223",
			message: "OK",
			err:     "",
		},
	}

	for _, tt := range tests {
		t.Run("should return expected PieceDeleteSummary values", func(t *testing.T) {
			assert := assert.New(t)

			// simulate piece stored with storagenode
			if err := TS.writeFile("11111111111111111111"); err != nil {
				t.Errorf("Error: %v\nCould not create test piece", err)
				return
			}

			// simulate piece TTL entry
			_, err := db.Exec(fmt.Sprintf(`INSERT INTO ttl (id, created, expires) VALUES ("%s", "%d", "%d")`, tt.id, 1234567890, 1234567890))
			assert.NoError(err)

			defer func() {
				_, err := db.Exec(fmt.Sprintf(`DELETE FROM ttl WHERE id="%s"`, tt.id))
				assert.NoError(err)
			}()

			defer func() {
				assert.NoError(TS.s.storage.Delete("11111111111111111111"))
			}()

			req := &pb.PieceDelete{Id: tt.id}
			resp, err := TS.c.Delete(ctx, req)

			if tt.err != "" {
				assert.Equal(tt.err, err.Error())
				return
			}

			assert.NoError(err)
			assert.Equal(tt.message, resp.GetMessage())

			// if test passes, check if file was indeed deleted
			filePath, err := TS.s.storage.PiecePath(tt.id)
			assert.NoError(err)
			if _, err = os.Stat(filePath); os.IsExist(err) {
				t.Errorf("File not deleted")
				return
			}
		})
	}
}

func newTestServerStruct(t *testing.T) (*Server, func()) {
	tmp, err := ioutil.TempDir("", "storj-piecestore")
	if err != nil {
		log.Fatalf("failed temp-dir: %v", err)
	}

	tempDBPath := filepath.Join(tmp, "test.db")
	tempDir := filepath.Join(tmp, "test-data", "3000")
	storage := pstore.NewStorage(tempDir)

	psDB, err := psdb.Open(context.TODO(), storage, tempDBPath)
	if err != nil {
		t.Fatalf("failed open psdb: %v", err)
	}

	verifier := func(authorization *pb.SignedMessage) error {
		return nil
	}
	server := &Server{
		log:              zaptest.NewLogger(t),
		storage:          storage,
		DB:               psDB,
		verifier:         verifier,
		totalAllocated:   math.MaxInt64,
		totalBwAllocated: math.MaxInt64,
	}
	return server, func() {
		if serr := server.Stop(context.TODO()); serr != nil {
			t.Fatal(serr)
		}
		// TODO:fix this error check
		_ = os.RemoveAll(tmp)
		// if err := os.RemoveAll(tmp); err != nil {
		// 	t.Fatal(err)
		// }
	}
}

func connect(addr string, o ...grpc.DialOption) (pb.PieceStoreRoutesClient, *grpc.ClientConn) {
	conn, err := grpc.Dial(addr, o...)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}

	c := pb.NewPieceStoreRoutesClient(conn)

	return c, conn
}

type TestServer struct {
	s        *Server
	scleanup func()
	grpcs    *grpc.Server
	conn     *grpc.ClientConn
	c        pb.PieceStoreRoutesClient
	k        crypto.PrivateKey
}

func NewTestServer(t *testing.T) *TestServer {
	check := func(e error) {
		if !assert.NoError(t, e) {
			t.Fail()
		}
	}

	caS, err := testidentity.NewTestCA(context.Background())
	check(err)
	fiS, err := caS.NewIdentity()
	check(err)
	so, err := fiS.ServerOption()
	check(err)

	caC, err := testidentity.NewTestCA(context.Background())
	check(err)
	fiC, err := caC.NewIdentity()
	check(err)
	co, err := fiC.DialOption(storj.NodeID{})
	check(err)

	s, cleanup := newTestServerStruct(t)
	grpcs := grpc.NewServer(so)

	k, ok := fiC.Key.(*ecdsa.PrivateKey)
	assert.True(t, ok)
	ts := &TestServer{s: s, scleanup: cleanup, grpcs: grpcs, k: k}
	addr := ts.start()
	ts.c, ts.conn = connect(addr, co)

	return ts
}

func (TS *TestServer) start() (addr string) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	pb.RegisterPieceStoreRoutesServer(TS.grpcs, TS.s)

	go func() {
		if err := TS.grpcs.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	return lis.Addr().String()
}

func (TS *TestServer) Stop() {
	if err := TS.conn.Close(); err != nil {
		panic(err)
	}
	TS.grpcs.Stop()
	TS.scleanup()
}

func serializeData(ba *pb.RenterBandwidthAllocation_Data) []byte {
	data, _ := proto.Marshal(ba)
	return data
}
