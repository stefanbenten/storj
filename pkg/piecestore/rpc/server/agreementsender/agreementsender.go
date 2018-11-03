// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package agreementsender

import (
	"flag"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"storj.io/storj/pkg/node"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/piecestore/rpc/server/psdb"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/utils"
)

var (
	defaultCheckInterval = flag.Duration("piecestore.agreementsender.check_interval", time.Hour, "number of seconds to sleep between agreement checks")
	defaultOverlayAddr   = flag.String("piecestore.agreementsender.overlay_addr", "127.0.0.1:7777", "Overlay Address")

	// ASError wraps errors returned from agreementsender package
	ASError = errs.Class("agreement sender error")
)

// AgreementSender maintains variables required for reading bandwidth agreements from a DB and sending them to a Payers
type AgreementSender struct {
	DB       *psdb.DB
	overlay  overlay.Client
	identity *provider.FullIdentity
	errs     []error
}

// Initialize the Agreement Sender
func Initialize(DB *psdb.DB, identity *provider.FullIdentity) (*AgreementSender, error) {
	overlay, err := overlay.NewOverlayClient(identity, *defaultOverlayAddr)
	if err != nil {
		return nil, err
	}

	return &AgreementSender{DB: DB, identity: identity, overlay: overlay}, nil
}

// Run the afreement sender with a context to cehck for cancel
func (as *AgreementSender) Run(ctx context.Context) error {
	zap.S().Info("AgreementSender is starting up")

	type agreementGroup struct {
		satellite  string
		agreements []*psdb.Agreement
	}

	c := make(chan *agreementGroup, 1)

	ticker := time.NewTicker(*defaultCheckInterval)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			agreementGroups, err := as.DB.GetBandwidthAllocations()
			if err != nil {
				zap.S().Error(err)
				continue
			}

			// Send agreements in groups by satellite id to open less connections
			for satellite, agreements := range agreementGroups {
				c <- &agreementGroup{satellite, agreements}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return utils.CombineErrors(as.errs...)
		case agreementGroup := <-c:
			go func() {
				zap.S().Info("Sending %v agreements to satellite %s\n", len(agreementGroup.agreements), agreementGroup.satellite)

				// Get satellite ip from overlay by Lookup agreementGroup.satellite
				satellite, err := as.overlay.Lookup(ctx, node.IDFromString(agreementGroup.satellite))
				if err != nil {
					zap.S().Error(err)
					return
				}

				// Create client from satellite ip
				identOpt, err := as.identity.DialOption()
				if err != nil {
					zap.S().Error(err)
					return
				}

				conn, err := grpc.Dial(satellite.GetAddress().String(), identOpt)
				if err != nil {
					zap.S().Error(err)
					return
				}

				client := pb.NewBandwidthClient(conn)
				stream, err := client.BandwidthAgreements(ctx)
				if err != nil {
					zap.S().Error(err)
					return
				}

				defer func() {
					if _, closeErr := stream.CloseAndRecv(); closeErr != nil {
						zap.S().Error("error closing stream %s :: %v.Send() = %v", closeErr, stream, closeErr)
					}
				}()

				for _, agreement := range agreementGroup.agreements {

					msg := &pb.RenterBandwidthAllocation{
						Data:      agreement.Agreement,
						Signature: agreement.Signature,
					}

					// Send agreement to satellite
					if err = stream.Send(msg); err != nil {
						zap.S().Error(err)
						return
					}

					// Delete from PSDB by signature
					if err = as.DB.DeleteBandwidthAllocationBySignature(agreement.Signature); err != nil {
						zap.S().Error(err)
						return
					}
				}
			}()
		}
	}
}
