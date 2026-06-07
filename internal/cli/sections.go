// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

func newSectionsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "sections",
		Short:       "View section search and detail pages",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newSectionsGetCmd(flags))
	cmd.AddCommand(newSectionsListCmd(flags))
	return cmd
}
