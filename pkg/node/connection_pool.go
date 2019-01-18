// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information

package node

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/zeebo/errs"
	"google.golang.org/grpc"

	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/pkg/transport"
	"storj.io/storj/pkg/utils"
)

// Error defines a connection pool error
var Error = errs.Class("connection pool error")

// ConnectionPool is the in memory pool of node connections
type ConnectionPool struct {
	tc    transport.Client
	mu    sync.RWMutex
	items map[storj.NodeID]*Conn
}

// Conn is the connection that is stored in the connection pool
type Conn struct {
	addr string

	dial   sync.Once
	client pb.NodesClient
	grpc   unsafe.Pointer //*grpc.ClientConn
	err    error
}

// NewConn intitalizes a new Conn struct with the provided address, but does not iniate a connection
func NewConn(addr string) *Conn { return &Conn{addr: addr} }

// NewConnectionPool initializes a new in memory pool
func NewConnectionPool(identity *provider.FullIdentity, obs ...transport.Observer) *ConnectionPool {
	return &ConnectionPool{
		tc:    transport.NewClient(identity, obs...),
		items: make(map[storj.NodeID]*Conn),
		mu:    sync.RWMutex{},
	}
}

// Get retrieves a node connection with the provided nodeID
// nil is returned if the NodeID is not in the connection pool
func (pool *ConnectionPool) Get(id storj.NodeID) (interface{}, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	i, ok := pool.items[id]
	if !ok {
		return nil, nil
	}

	return i, nil
}

// Disconnect deletes a connection associated with the provided NodeID
func (pool *ConnectionPool) Disconnect(id storj.NodeID) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	return pool.disconnect(id)

}

func (pool *ConnectionPool) disconnect(id storj.NodeID) error {
	conn, ok := pool.items[id]
	if !ok {
		return nil
	}

	ptr := atomic.LoadPointer(&conn.grpc)
	if ptr == nil {
		return nil
	}

	delete(pool.items, id)

	return (*grpc.ClientConn)(ptr).Close()
}

// Dial connects to the node with the given ID and Address returning a gRPC Node Client
func (pool *ConnectionPool) Dial(ctx context.Context, n *pb.Node) (pb.NodesClient, error) {
	id := n.Id
	pool.mu.Lock()
	conn, ok := pool.items[id]
	if !ok {
		conn = NewConn(n.GetAddress().Address)
		pool.items[id] = conn
	}
	pool.mu.Unlock()

	if n != nil {
		n.Type.DPanicOnInvalid("connection pool dial")
	}

	conn.dial.Do(func() {
		grpc, err := pool.tc.DialNode(ctx, n, grpc.WithBlock())
		conn.err = err
		if conn.err != nil {
			return
		}

		atomic.StorePointer(&conn.grpc, unsafe.Pointer(grpc))

		conn.client = pb.NewNodesClient(grpc)
	})

	if conn.err != nil {
		return nil, conn.err
	}

	return conn.client, nil
}

// DisconnectAll closes all connections nodes and removes them from the connection pool
func (pool *ConnectionPool) DisconnectAll() error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	errs := []error{}
	for k := range pool.items {
		if err := pool.disconnect(k); err != nil {
			errs = append(errs, Error.Wrap(err))
		}
	}

	return utils.CombineErrors(errs...)
}

// Init initializes the cache
func (pool *ConnectionPool) Init() {
	pool.mu.Lock()
	pool.items = make(map[storj.NodeID]*Conn)
	pool.mu.Unlock()
}
