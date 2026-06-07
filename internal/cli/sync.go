// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

// newSyncCmd was the generator's no-op stub. It now delegates to the
// hand-written newSyncRealCmd so root.go's wiring (rootCmd.AddCommand(newSyncCmd))
// still resolves without a second registration.
func newSyncCmd(flags *rootFlags) *cobra.Command {
	return newSyncRealCmd(flags)
}
