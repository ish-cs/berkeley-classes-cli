// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"

	"github.com/ish-cs/bcourses-cli/internal/bsource"
	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newSubjectsCmd(flags *rootFlags) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:         "subjects",
		Short:       "List every subject area facet (COMPSCI, MATH, ...) with section counts.",
		Example:     "  bcourses subjects\n  bcourses subjects --refresh --json",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			rows, err := store.ListBCSubjects(cmd.Context(), db.DB())
			if err != nil {
				return err
			}
			if refresh || len(rows) == 0 {
				snap, ferr := bsource.New(flags.timeout).FetchFacets(cmd.Context())
				if ferr != nil && len(rows) == 0 {
					return fmt.Errorf("fetch facets: %w", ferr)
				}
				if ferr == nil {
					_ = cacheFacets(cmd.Context(), db.DB(), snap)
					rows, _ = store.ListBCSubjects(cmd.Context(), db.DB())
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"count":    len(rows),
					"subjects": rows,
				})
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No subjects cached (and live fetch failed).")
				return nil
			}
			headers := []string{"ID", "Name"}
			tbl := make([][]string, 0, len(rows))
			for _, r := range rows {
				tbl = append(tbl, []string{r.ID, r.Name})
			}
			return flags.printTable(cmd, headers, tbl)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force live fetch even if cache is populated")
	return cmd
}
