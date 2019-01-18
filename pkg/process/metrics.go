// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package process

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	hw "github.com/jtolds/monkit-hw"
	"github.com/zeebo/admission/admproto"
	"go.uber.org/zap"
	"gopkg.in/spacemonkeygo/monkit.v2"
	"gopkg.in/spacemonkeygo/monkit.v2/environment"

	"storj.io/storj/pkg/identity"
	"storj.io/storj/pkg/telemetry"
)

var (
	metricInterval = flag.Duration("metrics.interval", telemetry.DefaultInterval,
		"how frequently to send up telemetry")
	metricCollector = flag.String("metrics.addr", "collectora.storj.io:9000",
		"address to send telemetry to")
	metricApp = flag.String("metrics.app", filepath.Base(os.Args[0]),
		"application name for telemetry identification")
	metricAppSuffix = flag.String("metrics.app-suffix", "-dev",
		"application suffix")
)

// InitMetrics initializes telemetry reporting. Makes a telemetry.Client and calls
// its Run() method in a goroutine.
func InitMetrics(ctx context.Context, r *monkit.Registry, instanceID string) (
	err error) {
	if *metricCollector == "" || *metricInterval == 0 {
		return Error.New("telemetry disabled")
	}
	if r == nil {
		r = monkit.Default
	}
	if instanceID == "" {
		instanceID = telemetry.DefaultInstanceID()
	}
	c, err := telemetry.NewClient(*metricCollector, telemetry.ClientOpts{
		Interval:      *metricInterval,
		Application:   *metricApp + *metricAppSuffix,
		Instance:      instanceID,
		Registry:      r,
		FloatEncoding: admproto.Float32Encoding,
	})
	if err != nil {
		return err
	}
	environment.Register(r)
	hw.Register(r)
	go c.Run(ctx)
	return nil
}

// InitMetricsWithCertPath initializes telemetry reporting, using the node ID
// corresponding to the given certificate as the telemetry instance ID.
func InitMetricsWithCertPath(ctx context.Context, r *monkit.Registry, certPath string) error {
	var metricsID string
	nodeID, err := identity.NodeIDFromCertPath(certPath)
	if err != nil {
		zap.S().Errorf("Could not read identity for telemetry setup: %v", err)
		metricsID = "" // InitMetrics() will fill in a default value
	} else {
		metricsID = nodeID.String()
	}
	return InitMetrics(ctx, r, metricsID)
}
