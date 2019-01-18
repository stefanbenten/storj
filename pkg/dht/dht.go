// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package dht

import (
	"context"
	"time"

	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/storage"
)

// DHT is the interface for the DHT in the Storj network
type DHT interface {
	GetNodes(ctx context.Context, start storj.NodeID, limit int, restrictions ...pb.Restriction) ([]*pb.Node, error)
	GetRoutingTable(ctx context.Context) (RoutingTable, error)
	Bootstrap(ctx context.Context) error
	Ping(ctx context.Context, node pb.Node) (pb.Node, error)
	FindNode(ctx context.Context, ID storj.NodeID) (pb.Node, error)
	Disconnect() error
	Seen() []*pb.Node
}

// RoutingTable contains information on nodes we have locally
type RoutingTable interface {
	// local params
	Local() pb.Node
	K() int
	CacheSize() int
	GetNodes(id storj.NodeID) (nodes []*pb.Node, ok bool)
	GetBucketIds() (storage.Keys, error)
	FindNear(id storj.NodeID, limit int) ([]*pb.Node, error)
	ConnectionSuccess(node *pb.Node) error
	ConnectionFailed(node *pb.Node) error
	// these are for refreshing
	SetBucketTimestamp(id []byte, now time.Time) error
	GetBucketTimestamp(id []byte) (time.Time, error)
}

// Bucket is a set of methods to act on kademlia k buckets
type Bucket interface {
	Routing() []pb.Node
	Cache() []pb.Node
	// TODO: should this be a NodeID?
	Midpoint() string
	Nodes() []*pb.Node
}
