// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/ish-cs/berkeley-classes-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelScheduleBuildCmd(flags *rootFlags) *cobra.Command {
	var flagTerm string
	var flagCourse []string
	var flagMaxResults int

	cmd := &cobra.Command{
		Use:         "build",
		Short:       "Build a valid weekly schedule from a wishlist of courses with no time overlaps.",
		Example:     "  berkeley-classes schedule build --term 'Fall 2026' --course 'COMPSCI 61A' --course 'MATH 1B' --course 'ENGLISH 45A'\n  berkeley-classes schedule build --term 'Fall 2026' --course 'COMPSCI 61A' --course 'MATH 53' --max-results 5",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(flagCourse) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("at least one --course required (e.g. --course 'COMPSCI 61A')"))
			}
			if len(flagCourse) > 7 {
				return usageErr(fmt.Errorf("too many --course inputs (max 7) — combinatorial explosion otherwise"))
			}
			if flagMaxResults <= 0 {
				flagMaxResults = 5
			}

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

			courseSections := make([][]store.BCSection, 0, len(flagCourse))
			for _, c := range flagCourse {
				rows, err := store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
					CourseCode: c,
					TermID:     termID,
					Limit:      200,
				})
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					return notFoundErr(fmt.Errorf("no sections for course %q in local store (run 'sync' first)", c))
				}
				// Deduplicate by CCN; only consider sections with parseable
				// meeting times — async sections never conflict with anyone
				// and would inflate the search space.
				seen := map[int]bool{}
				kept := make([]store.BCSection, 0)
				for _, r := range rows {
					if seen[r.CCN] {
						continue
					}
					seen[r.CCN] = true
					kept = append(kept, r)
				}
				courseSections = append(courseSections, kept)
			}

			// Hard combinatorial cap before product expansion
			total := 1
			for _, opts := range courseSections {
				total *= len(opts)
				if total > 10000 {
					return fmt.Errorf("input space too large (>10k combinations); narrow with fewer --course or use --term to bound the search")
				}
			}

			type combo struct {
				Sections []store.BCSection `json:"sections"`
			}
			results := make([]combo, 0, flagMaxResults)

			// Recursive product walk so we can short-circuit on partial conflict.
			var walk func(idx int, picked []store.BCSection)
			walk = func(idx int, picked []store.BCSection) {
				if len(results) >= flagMaxResults {
					return
				}
				if idx == len(courseSections) {
					out := make([]store.BCSection, len(picked))
					copy(out, picked)
					results = append(results, combo{Sections: out})
					return
				}
				for _, cand := range courseSections[idx] {
					if hasConflict(picked, cand) {
						continue
					}
					walk(idx+1, append(picked, cand))
				}
			}
			walk(0, make([]store.BCSection, 0, len(courseSections)))

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"requested":         flagCourse,
					"term":              flagTerm,
					"max_results":       flagMaxResults,
					"combinations":      results,
					"combination_count": len(results),
				})
			}

			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No conflict-free schedules found.")
				return nil
			}
			for i, c := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "Option %d\n", i+1)
				for _, s := range c.Sections {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-15s %-5s #%d  %s  %s\n",
						s.CourseCode, s.SectionType, s.CCN, s.MeetingDays, s.MeetingTime)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagTerm, "term", "", "Term name (e.g. 'Fall 2026') to filter sections")
	cmd.Flags().StringSliceVar(&flagCourse, "course", nil, "Course code (e.g. 'COMPSCI 61A'); repeat for each wishlist item")
	cmd.Flags().IntVar(&flagMaxResults, "max-results", 5, "Maximum number of conflict-free schedules to emit")
	_ = strings.TrimSpace // keep imports honest
	return cmd
}

// hasConflict returns true if cand overlaps with any section in picked.
func hasConflict(picked []store.BCSection, cand store.BCSection) bool {
	candDays := parseMeetingDays(cand.MeetingDays)
	candStart, candEnd, candOK := parseMeetingTime(cand.MeetingTime)
	if !candOK || len(candDays) == 0 {
		return false // async; never conflicts
	}
	for _, p := range picked {
		pDays := parseMeetingDays(p.MeetingDays)
		pStart, pEnd, pOK := parseMeetingTime(p.MeetingTime)
		if !pOK || len(pDays) == 0 {
			continue
		}
		if len(intersectDays(candDays, pDays)) == 0 {
			continue
		}
		if candStart < pEnd && pStart < candEnd {
			return true
		}
	}
	return false
}
