// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"storj.io/storj/cmd/uplink/cmd"
	"storj.io/storj/pkg/process"
)

func main() {
	process.Exec(cmd.CLICmd)
}
