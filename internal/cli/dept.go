// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelDeptCmd(flags *rootFlags) *cobra.Command {
	var flagTerm string
	var flagTopInstructors int

	cmd := &cobra.Command{
		Use:         "dept <code-or-name>",
		Short:       "Department overview: total offerings, total seats, top instructors.",
		Example:     "  bcourses dept COMPSCI --term 'Fall 2026'\n  bcourses dept 'Computer Science' --agent",
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
				return usageErr(fmt.Errorf("dept requires a department code or name, e.g. 'COMPSCI' or 'Computer Science'"))
			}
			query := strings.TrimSpace(args[0])
			if query == "" {
				return usageErr(fmt.Errorf("department code/name cannot be empty"))
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

			rows, err := store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
				CourseCodeLike:  strings.ToUpper(query) + " %",
				TermID:          termID,
				SubjectNameLike: "%" + query + "%",
				Limit:           5000,
			})
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				rows, err = store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
					CourseCodeLike: strings.ToUpper(query) + " %",
					TermID:         termID,
					Limit:          5000,
				})
				if err != nil {
					return err
				}
			}
			if len(rows) == 0 {
				rows, err = store.QuerySections(cmd.Context(), db.DB(), store.QuerySectionsOpts{
					SubjectNameLike: "%" + query + "%",
					TermID:          termID,
					Limit:           5000,
				})
				if err != nil {
					return err
				}
			}

			top := flagTopInstructors
			if top <= 0 {
				top = 5
			}
			stats := summarizeDept(rows, top)
			stats.Query = query
			stats.Term = flagTerm

			if flags.asJSON {
				return flags.printJSON(cmd, stats)
			}
			if stats.SectionCount == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No sections found for department %q. Run 'bcourses sync --term <term> --subject %q' first.\n", query, query)
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Department: %s", stats.Query)
			if stats.Term != "" {
				fmt.Fprintf(out, "  (term: %s)", stats.Term)
			}
			fmt.Fprintln(out)
			fmt.Fprintf(out, "Sections:      %d\n", stats.SectionCount)
			fmt.Fprintf(out, "Distinct courses: %d\n", stats.DistinctCourses)
			fmt.Fprintf(out, "Open seats:    %d\n", stats.TotalOpenSeats)
			fmt.Fprintf(out, "Enrolled:      %d\n", stats.TotalEnrolled)
			fmt.Fprintf(out, "Capacity:      %d\n", stats.TotalCapacity)
			if stats.TotalCapacity > 0 {
				fmt.Fprintf(out, "Fill rate:     %.0f%%\n", 100*float64(stats.TotalEnrolled)/float64(stats.TotalCapacity))
			}
			if len(stats.TopInstructors) > 0 {
				fmt.Fprintf(out, "Top instructors (by section count):\n")
				headers := []string{"Instructor", "Sections"}
				tbl := make([][]string, 0, len(stats.TopInstructors))
				for _, ti := range stats.TopInstructors {
					tbl = append(tbl, []string{ti.Name, fmt.Sprintf("%d", ti.Sections)})
				}
				return flags.printTable(cmd, headers, tbl)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagTerm, "term", "", "Term name (e.g. 'Fall 2026') to scope the overview")
	cmd.Flags().IntVar(&flagTopInstructors, "top", 5, "Number of top instructors to list")
	return cmd
}

type deptInstructor struct {
	Name     string `json:"name"`
	Sections int    `json:"sections"`
}

type deptStats struct {
	Query           string           `json:"query"`
	Term            string           `json:"term,omitempty"`
	SectionCount    int              `json:"section_count"`
	DistinctCourses int              `json:"distinct_courses"`
	TotalOpenSeats  int              `json:"total_open_seats"`
	TotalEnrolled   int              `json:"total_enrolled"`
	TotalCapacity   int              `json:"total_capacity"`
	TotalWaitlisted int              `json:"total_waitlisted"`
	TopInstructors  []deptInstructor `json:"top_instructors"`
}

func summarizeDept(rows []store.BCSection, top int) deptStats {
	stats := deptStats{TopInstructors: []deptInstructor{}}
	courses := map[string]struct{}{}
	instructorCounts := map[string]int{}
	for _, r := range rows {
		stats.SectionCount++
		if r.CourseCode != "" {
			courses[r.CourseCode] = struct{}{}
		}
		stats.TotalOpenSeats += r.OpenSeats
		stats.TotalEnrolled += r.Enrolled
		stats.TotalCapacity += r.Capacity
		stats.TotalWaitlisted += r.Waitlisted
		for _, name := range splitInstructors(r.Instructors) {
			if name == "" {
				continue
			}
			instructorCounts[name]++
		}
	}
	stats.DistinctCourses = len(courses)
	for name, count := range instructorCounts {
		stats.TopInstructors = append(stats.TopInstructors, deptInstructor{Name: name, Sections: count})
	}
	sort.Slice(stats.TopInstructors, func(i, j int) bool {
		if stats.TopInstructors[i].Sections != stats.TopInstructors[j].Sections {
			return stats.TopInstructors[i].Sections > stats.TopInstructors[j].Sections
		}
		return stats.TopInstructors[i].Name < stats.TopInstructors[j].Name
	})
	if len(stats.TopInstructors) > top {
		stats.TopInstructors = stats.TopInstructors[:top]
	}
	return stats
}

func splitInstructors(s string) []string {
	if s == "" {
		return nil
	}
	for _, sep := range []string{";", "/", "|"} {
		s = strings.ReplaceAll(s, sep, ",")
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.EqualFold(p, "Staff") || strings.EqualFold(p, "TBA") {
			continue
		}
		out = append(out, p)
	}
	return out
}
