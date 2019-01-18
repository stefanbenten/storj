// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package discovery

import (
	"context"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/pkg/kademlia"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/statdb"
)

var (
	mon = monkit.Package()
	// Error represents an overlay error
	Error = errs.Class("discovery error")
)

// Config loads on the configuration values from run flags
type Config struct {
	RefreshInterval time.Duration `help:"the interval at which the cache refreshes itself in seconds" default:"1s"`
}

// Run runs the Discovery boot up and initialization
func (c Config) Run(ctx context.Context, server *provider.Provider) (err error) {
	defer mon.Task()(&ctx)(&err)

	overlay := overlay.LoadFromContext(ctx)
	if overlay == nil {
		return Error.New("programmer error: overlay responsibility unstarted")
	}
	kad := kademlia.LoadFromContext(ctx)
	if kad == nil {
		return Error.New("programmer error: kademlia responsibility unstarted")
	}
	stat, ok := ctx.Value("masterdb").(interface {
		StatDB() statdb.DB
	})

	if !ok {
		return Error.New("unable to get master db instance")
	}

	discovery := NewDiscovery(zap.L().Named("discovery"), overlay, kad, stat.StatDB())

	zap.L().Debug("Starting discovery")

	ticker := time.NewTicker(c.RefreshInterval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				err := discovery.Refresh(ctx)
				if err != nil {
					discovery.log.Error("Error with cache refresh: ", zap.Error(err))
				}

				err = discovery.Discovery(ctx)
				if err != nil {
					discovery.log.Error("Error with cache discovery: ", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return server.Run(ctx)
}
