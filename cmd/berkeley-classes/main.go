// Copyright 2026 ish-cs. MIT License. See LICENSE.

package main

import (
	"os"

	"github.com/ish-cs/berkeley-classes-cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
