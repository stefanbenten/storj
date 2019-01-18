// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"storj.io/storj/internal/fpath"
	"storj.io/storj/pkg/process"
)

func init() {
	addCmd(&cobra.Command{
		Use:   "cat",
		Short: "Copies a Storj object to standard out",
		RunE:  catMain,
	}, CLICmd)
}

// catMain is the function executed when catCmd is called
func catMain(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return fmt.Errorf("No object specified for copy")
	}

	ctx := process.Ctx(cmd)

	src, err := fpath.New(args[0])
	if err != nil {
		return err
	}

	if src.IsLocal() {
		return fmt.Errorf("No bucket specified, use format sj://bucket/")
	}

	dst, err := fpath.New("-")
	if err != nil {
		return err
	}

	return download(ctx, src, dst, false)
}
