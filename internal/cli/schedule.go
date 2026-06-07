// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelScheduleCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "schedule",
		Short:       "Build conflict-free Berkeley schedules from a wishlist of courses.",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelScheduleBuildCmd(flags))
	return cmd
}
