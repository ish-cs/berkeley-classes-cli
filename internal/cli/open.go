// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/ish-cs/berkeley-classes-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelOpenCmd(flags *rootFlags) *cobra.Command {
	var flagTerm string

	cmd := &cobra.Command{
		Use:         "open <course-code>",
		Short:       "Show every open section of a course in one command, including waitlist length.",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("open requires a course code, e.g. 'COMPSCI 61A'"))
			}
			code := strings.TrimSpace(args[0])

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("berkeley-classes"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			termID := ""
			if flagTerm != "" {
				termID, _ = store.FindTermIDByName(cmd.Context(), db.DB(), flagTerm)
			}

			rows, err := store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
				CourseCode: code,
				TermID:     termID,
				OpenOnly:   true,
				Limit:      200,
			})
			if err != nil {
				return err
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"course":   code,
					"term":     flagTerm,
					"count":    len(rows),
					"sections": rows,
				})
			}

			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No open sections found for", code,
					"(run 'sync' to refresh local data).")
				return nil
			}
			headers := []string{"CCN", "Course", "Type", "Sec", "Open", "Enrolled", "Waitlist", "Cap", "Days", "Time"}
			tbl := make([][]string, 0, len(rows))
			for _, r := range rows {
				tbl = append(tbl, []string{
					fmt.Sprintf("%d", r.CCN), r.CourseCode, r.SectionType, r.SectionNumber,
					fmt.Sprintf("%d", r.OpenSeats),
					fmt.Sprintf("%d", r.Enrolled),
					fmt.Sprintf("%d", r.Waitlisted),
					fmt.Sprintf("%d", r.Capacity),
					r.MeetingDays, r.MeetingTime,
				})
			}
			return flags.printTable(cmd, headers, tbl)
		},
	}
	cmd.Flags().StringVar(&flagTerm, "term", "", "Term name (e.g. 'Fall 2026') to limit search")
	return cmd
}
