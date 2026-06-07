// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ish-cs/berkeley-classes-cli/internal/bsource"
	"github.com/ish-cs/berkeley-classes-cli/internal/store"
	"github.com/spf13/cobra"
)

func newTermsCmd(flags *rootFlags) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:         "terms",
		Short:       "List every Berkeley term (Fall 2026, Summer A, etc.) with section counts.",
		Example:     "  berkeley-classes terms\n  berkeley-classes terms --refresh --json",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("berkeley-classes"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			rows, err := store.ListBCTerms(cmd.Context(), db.DB())
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
					rows, _ = store.ListBCTerms(cmd.Context(), db.DB())
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"count": len(rows),
					"terms": rows,
				})
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No terms cached (and live fetch failed).")
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

// cacheFacets persists every facet from snap into bc_terms / bc_subjects.
func cacheFacets(ctx context.Context, db *sql.DB, snap *bsource.FacetSnapshot) error {
	for _, t := range snap.Terms {
		if err := store.UpsertTerm(ctx, db, t.ID, t.Name, ""); err != nil {
			return err
		}
	}
	for _, s := range snap.SubjectAreas {
		if err := store.UpsertSubject(ctx, db, s.ID, s.Name, ""); err != nil {
			return err
		}
	}
	return nil
}
