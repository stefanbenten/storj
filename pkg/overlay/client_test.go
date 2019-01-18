// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package overlay_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testidentity"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storj"
)

func TestNewClient(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	cases := []struct {
		address string
	}{
		{
			address: "127.0.0.1:8080",
		},
	}

	for _, v := range cases {
		ca, err := testidentity.NewTestCA(ctx)
		assert.NoError(t, err)
		identity, err := ca.NewIdentity()
		assert.NoError(t, err)

		oc, err := overlay.NewClient(identity, v.address)
		assert.NoError(t, err)

		assert.NotNil(t, oc)
	}
}

func TestChoose(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 4, 1)
	require.NoError(t, err)

	planet.Start(ctx)
	// we wait a second for all the nodes to complete bootstrapping off the satellite
	time.Sleep(2 * time.Second)
	defer ctx.Check(planet.Shutdown)

	oc, err := planet.Uplinks[0].DialOverlay(planet.Satellites[0])
	if err != nil {
		t.Fatal(err)
	}

	n1 := &pb.Node{Id: storj.NodeID{1}, Type: pb.NodeType_STORAGE}
	n2 := &pb.Node{Id: storj.NodeID{2}, Type: pb.NodeType_STORAGE}
	n3 := &pb.Node{Id: storj.NodeID{3}, Type: pb.NodeType_STORAGE}
	n4 := &pb.Node{Id: storj.NodeID{4}, Type: pb.NodeType_STORAGE}
	n5 := &pb.Node{Id: storj.NodeID{5}, Type: pb.NodeType_STORAGE}
	n6 := &pb.Node{Id: storj.NodeID{6}, Type: pb.NodeType_STORAGE}
	n7 := &pb.Node{Id: storj.NodeID{7}, Type: pb.NodeType_STORAGE}
	n8 := &pb.Node{Id: storj.NodeID{8}, Type: pb.NodeType_STORAGE}

	id1 := storj.NodeID{1}
	id2 := storj.NodeID{2}
	id3 := storj.NodeID{3}
	id4 := storj.NodeID{4}

	cases := []struct {
		limit        int
		space        int64
		bandwidth    int64
		uptime       float64
		uptimeCount  int64
		auditSuccess float64
		auditCount   int64
		allNodes     []*pb.Node
		excluded     storj.NodeIDList
	}{
		{
			limit:        4,
			space:        0,
			bandwidth:    0,
			uptime:       0,
			uptimeCount:  0,
			auditSuccess: 0,
			auditCount:   0,
			allNodes:     []*pb.Node{n1, n2, n3, n4, n5, n6, n7, n8},
			excluded:     storj.NodeIDList{id1, id2, id3, id4},
		},
	}

	for _, v := range cases {
		newNodes, err := oc.Choose(ctx, overlay.Options{
			Amount:       v.limit,
			Space:        v.space,
			Uptime:       v.uptime,
			UptimeCount:  v.uptimeCount,
			AuditSuccess: v.auditSuccess,
			AuditCount:   v.auditCount,
			Excluded:     v.excluded,
		})
		assert.NoError(t, err)

		excludedNodes := make(map[storj.NodeID]bool)
		for _, e := range v.excluded {
			excludedNodes[e] = true
		}
		assert.Len(t, newNodes, v.limit)
		for _, n := range newNodes {
			assert.NotContains(t, excludedNodes, n.Id)
			assert.True(t, n.GetRestrictions().GetFreeDisk() >= v.space)
			assert.True(t, n.GetRestrictions().GetFreeBandwidth() >= v.bandwidth)
			assert.True(t, n.GetReputation().GetUptimeRatio() >= v.uptime)
			assert.True(t, n.GetReputation().GetUptimeCount() >= v.uptimeCount)
			assert.True(t, n.GetReputation().GetAuditSuccessRatio() >= v.auditSuccess)
			assert.True(t, n.GetReputation().GetAuditCount() >= v.auditCount)

		}
	}
}

func TestLookup(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 4, 1)
	require.NoError(t, err)

	planet.Start(ctx)
	// we wait a second for all the nodes to complete bootstrapping off the satellite
	time.Sleep(2 * time.Second)
	defer ctx.Check(planet.Shutdown)

	oc, err := planet.Uplinks[0].DialOverlay(planet.Satellites[0])
	if err != nil {
		t.Fatal(err)
	}

	nid1 := planet.StorageNodes[0].ID()

	cases := []struct {
		nodeID    storj.NodeID
		expectErr bool
	}{
		{
			nodeID:    nid1,
			expectErr: false,
		},
		{
			nodeID:    storj.NodeID{1},
			expectErr: true,
		},
	}

	for _, v := range cases {
		n, err := oc.Lookup(ctx, v.nodeID)
		if v.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, n.Id.String(), v.nodeID.String())
		}
	}

}

func TestBulkLookup(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 4, 1)
	require.NoError(t, err)

	planet.Start(ctx)
	// we wait a second for all the nodes to complete bootstrapping off the satellite
	time.Sleep(2 * time.Second)
	defer ctx.Check(planet.Shutdown)

	oc, err := planet.Uplinks[0].DialOverlay(planet.Satellites[0])
	if err != nil {
		t.Fatal(err)
	}

	nid1 := planet.StorageNodes[0].ID()
	nid2 := planet.StorageNodes[1].ID()
	nid3 := planet.StorageNodes[2].ID()

	cases := []struct {
		nodeIDs       storj.NodeIDList
		expectedCalls int
	}{
		{
			nodeIDs:       storj.NodeIDList{nid1, nid2, nid3},
			expectedCalls: 1,
		},
	}
	for _, v := range cases {
		resNodes, err := oc.BulkLookup(ctx, v.nodeIDs)
		assert.NoError(t, err)
		for i, n := range resNodes {
			assert.Equal(t, n.Id, v.nodeIDs[i])
		}
		assert.Equal(t, len(resNodes), len(v.nodeIDs))
	}
}

func TestBulkLookupV2(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 4, 1)
	require.NoError(t, err)

	planet.Start(ctx)
	// we wait a second for all the nodes to complete bootstrapping off the satellite
	time.Sleep(2 * time.Second)
	defer ctx.Check(planet.Shutdown)

	oc, err := planet.Uplinks[0].DialOverlay(planet.Satellites[0])
	if err != nil {
		t.Fatal(err)
	}

	cache := planet.Satellites[0].Overlay

	nid1 := storj.NodeID{1}
	nid2 := storj.NodeID{2}
	nid3 := storj.NodeID{3}
	nid4 := storj.NodeID{4}
	nid5 := storj.NodeID{5}

	n1 := &pb.Node{Id: storj.NodeID{1}}
	n2 := &pb.Node{Id: storj.NodeID{2}}
	n3 := &pb.Node{Id: storj.NodeID{3}}

	nodes := []*pb.Node{n1, n2, n3}
	for _, n := range nodes {
		assert.NoError(t, cache.Put(ctx, n.Id, *n))
	}

	{ // empty id
		_, err := oc.BulkLookup(ctx, storj.NodeIDList{})
		assert.Error(t, err)
	}

	{ // valid ids
		idList := storj.NodeIDList{nid1, nid2, nid3}
		ns, err := oc.BulkLookup(ctx, idList)
		assert.NoError(t, err)

		for i, n := range ns {
			assert.Equal(t, n.Id, idList[i])
		}
	}

	{ // missing ids
		idList := storj.NodeIDList{nid4, nid5}
		ns, err := oc.BulkLookup(ctx, idList)
		assert.NoError(t, err)

		assert.Equal(t, []*pb.Node{nil, nil}, ns)
	}

	{ // different order and missing
		idList := storj.NodeIDList{nid3, nid4, nid1, nid2, nid5}
		ns, err := oc.BulkLookup(ctx, idList)
		assert.NoError(t, err)

		expectedNodes := []*pb.Node{n3, nil, n1, n2, nil}
		for i, n := range ns {
			if n == nil {
				assert.Nil(t, expectedNodes[i])
			} else {
				assert.Equal(t, n.Id, expectedNodes[i].Id)
			}
		}
	}
}
