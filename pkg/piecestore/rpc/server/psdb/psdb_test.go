// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package psdb

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gogo/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"
	pb "storj.io/storj/protos/piecestore"

	"golang.org/x/net/context"
)

var ctx = context.Background()

const concurrency = 10

func TestHappyPath(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "storj-psdb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	dbpath := filepath.Join(tmpdir, "psdb.db")

	db, err := Open(ctx, "", dbpath)
	if err != nil {
		t.Fatal(err)
	}

	type TTL struct {
		ID         string
		Expiration int64
	}

	tests := []TTL{
		{ID: "", Expiration: 0},
		{ID: "\x00", Expiration: ^int64(0)},
		{ID: "test", Expiration: 666},
	}

	t.Run("Add", func(t *testing.T) {
		for P := 0; P < concurrency; P++ {
			t.Run("#"+strconv.Itoa(P), func(t *testing.T) {
				t.Parallel()
				for _, ttl := range tests {
					err := db.AddTTLToDB(ttl.ID, ttl.Expiration)
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	})

	t.Run("Get", func(t *testing.T) {
		for P := 0; P < concurrency; P++ {
			t.Run("#"+strconv.Itoa(P), func(t *testing.T) {
				t.Parallel()
				for _, ttl := range tests {
					expiration, err := db.GetTTLByID(ttl.ID)
					if err != nil {
						t.Fatal(err)
					}

					if ttl.Expiration != expiration {
						t.Fatalf("expected %d got %d", ttl.Expiration, expiration)
					}
				}
			})
		}
	})

	t.Run("Delete", func(t *testing.T) {
		for P := 0; P < concurrency; P++ {
			t.Run("Delete", func(t *testing.T) {
				t.Parallel()
				for _, ttl := range tests {
					err := db.DeleteTTLByID(ttl.ID)
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	})

	t.Run("Get Deleted", func(t *testing.T) {
		for P := 0; P < concurrency; P++ {
			t.Run("#"+strconv.Itoa(P), func(t *testing.T) {
				t.Parallel()
				for _, ttl := range tests {
					expiration, err := db.GetTTLByID(ttl.ID)
					if err == nil {
						t.Fatal(err)
					}
					if expiration != 0 {
						t.Fatalf("expected expiration 0 got %d", expiration)
					}
				}
			})
		}
	})

	serialize := func(v proto.Message) []byte {
		data, err := proto.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	bandwidthAllocation := func(total int64) []byte {
		return serialize(&pb.RenterBandwidthAllocation_Data{
			PayerAllocation: &pb.PayerBandwidthAllocation{},
			Total:           total,
		})
	}

	//TODO: use better data
	allocationTests := []*pb.RenterBandwidthAllocation{
		{
			Signature: []byte("signed by test"),
			Data:      bandwidthAllocation(0),
		},
		{
			Signature: []byte("signed by sigma"),
			Data:      bandwidthAllocation(10),
		},
		{
			Signature: []byte("signed by sigma"),
			Data:      bandwidthAllocation(98),
		},
		{
			Signature: []byte("signed by test"),
			Data:      bandwidthAllocation(3),
		},
	}

	t.Run("Bandwidth Allocation", func(t *testing.T) {
		for P := 0; P < concurrency; P++ {
			t.Run("#"+strconv.Itoa(P), func(t *testing.T) {
				t.Parallel()
				for _, test := range allocationTests {
					err := db.WriteBandwidthAllocToDB(test)
					if err != nil {
						t.Fatal(err)
					}

					agreements, err := db.GetBandwidthAllocationBySignature(test.Signature)
					if err != nil {
						t.Fatal(err)
					}

					found := false
					for _, agreement := range agreements {
						if bytes.Equal(agreement, test.Data) {
							found = true
							break
						}
					}

					if !found {
						t.Fatal("did not find added bandwidth allocation")
					}
				}
			})
		}
	})
}
