// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information

package sync2

import (
	"context"
	"io"
)

type readerFunc func(p []byte) (n int, err error)

func (rf readerFunc) Read(p []byte) (n int, err error) { return rf(p) }

// Copy implements copying with cancellation
func Copy(ctx context.Context, dst io.Writer, src io.Reader) (written int64, err error) {
	written, err = io.Copy(dst, readerFunc(func(p []byte) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			return src.Read(p)
		}
	}))

	return written, err
}
