// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information

package testplanet

import (
	"io"
	"net"

	"storj.io/storj/pkg/kademlia"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/pointerdb/pdbclient"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/transport"
	"storj.io/storj/pkg/utils"
	"storj.io/storj/storage/teststore"
)

// Node is a general purpose
type Node struct {
	Info      pb.Node
	Identity  *provider.FullIdentity
	Transport *transport.Transport
	Listener  net.Listener
	Provider  *provider.Provider
	Kademlia  *kademlia.Kademlia
	Overlay   *overlay.Cache

	Dependencies []io.Closer
}

// ID returns node id
// TODO: switch to storj.NodeID
func (node *Node) ID() string { return node.Info.Id }

// Addr retursn node address
func (node *Node) Addr() string { return node.Info.Address.Address }

// Shutdown shuts down all node dependencies
func (node *Node) Shutdown() error {
	var errs []error
	if node.Provider != nil {
		errs = append(errs, node.Provider.Close())
	}
	// Provider automatically closes listener
	// if node.Listener != nil {
	//    errs = append(errs, node.Listener.Close())
	// }
	if node.Kademlia != nil {
		errs = append(errs, node.Kademlia.Disconnect())
	}

	for _, dep := range node.Dependencies {
		err := dep.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return utils.CombineErrors(errs...)
}

// DialPointerDB dials destination with apikey and returns pointerdb Client
func (node *Node) DialPointerDB(destination *Node, apikey string) (pdbclient.Client, error) {
	// TODO: use node.Transport instead of pdbclient.NewClient
	/*
		conn, err := node.Transport.DialNode(context.Background(), &destination.Info)
		if err != nil {
			return nil, err
		}
		return piececlient.NewPSClient
	*/
	return pdbclient.NewClient(node.Identity, destination.Addr(), apikey)
}

// initOverlay creates overlay for a given planet
func (node *Node) initOverlay(planet *Planet) error {
	routing, err := kademlia.NewRoutingTable(node.Info, teststore.New(), teststore.New())
	if err != nil {
		return err
	}

	kad, err := kademlia.NewKademliaWithRoutingTable(node.Info, planet.nodeInfos, node.Identity, 5, routing)
	if err != nil {
		return utils.CombineErrors(err, routing.Close())
	}

	node.Kademlia = kad
	node.Overlay = overlay.NewOverlayCache(teststore.New(), node.Kademlia)

	return nil
}

type closerFunc func() error

func (fn closerFunc) Close() error { return fn() }
