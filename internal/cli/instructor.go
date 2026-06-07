// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelInstructorCmd(flags *rootFlags) *cobra.Command {
	var flagTerm string
	var flagLimit int

	cmd := &cobra.Command{
		Use:         "instructor <name-fragment>",
		Short:       "List every section a given instructor is teaching this term, across every subject.",
		Example:     "  bcourses instructor 'John DeNero' --term 'Fall 2026'\n  bcourses instructor DeNero --agent",
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
				return usageErr(fmt.Errorf("instructor requires a name fragment, e.g. 'DeNero'"))
			}
			name := strings.TrimSpace(args[0])
			if name == "" {
				return usageErr(fmt.Errorf("name fragment cannot be empty"))
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
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

			limit := flagLimit
			if limit <= 0 {
				limit = 200
			}
			rows, err := store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
				InstructorLike: "%" + name + "%",
				TermID:         termID,
				Limit:          limit,
			})
			if err != nil {
				return err
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"query":    name,
					"term":     flagTerm,
					"count":    len(rows),
					"sections": rows,
				})
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No sections found for", name)
				return nil
			}
			headers := []string{"CCN", "Course", "Type", "Sec", "Title", "Instructors", "Days", "Time"}
			tbl := make([][]string, 0, len(rows))
			for _, r := range rows {
				tbl = append(tbl, []string{
					fmt.Sprintf("%d", r.CCN), r.CourseCode, r.SectionType, r.SectionNumber,
					truncate(r.Title, 30), truncate(r.Instructors, 30), r.MeetingDays, r.MeetingTime,
				})
			}
			return flags.printTable(cmd, headers, tbl)
		},
	}
	cmd.Flags().StringVar(&flagTerm, "term", "", "Term name (e.g. 'Fall 2026') to limit search")
	cmd.Flags().IntVar(&flagLimit, "limit", 200, "Max sections to return")
	return cmd
}
