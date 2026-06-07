// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/ish-cs/bcourses-cli/internal/bsource"
	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

// newSearchRealCmd is the hand-written `find` command. The generator
// reserves "search" for its endpoint-mirror shape, so this lives under a
// separate verb.
func newSearchRealCmd(flags *rootFlags) *cobra.Command {
	var keywords, term, subject string
	var openOnly bool
	var limit, maxScanPages int

	cmd := &cobra.Command{
		Use:         "find",
		Short:       "Search every Berkeley class across all subjects and terms.",
		Long:        "Hand-written search against https://classes.berkeley.edu/search/class. Uses local store for term/subject resolution when available, otherwise falls back to live facet lookup.",
		Example:     "  bcourses find --subject 'Computer Science' --term 'Fall 2026' --open-only\n  bcourses find --keywords 'CS 61A' --term 'Fall 2026' --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 && len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if keywords == "" && term == "" && subject == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("find requires at least one of --keywords, --term, --subject"))
			}
			if limit <= 0 {
				limit = 20
			}
			if maxScanPages <= 0 {
				maxScanPages = 5
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			termID := term
			if termID != "" && !isAllDigits(termID) {
				resolved, _ := store.FindTermIDByName(cmd.Context(), db.DB(), term)
				if resolved == "" {
					// Live fallback
					if snap, ferr := bsource.New(flags.timeout).FetchFacets(cmd.Context()); ferr == nil {
						resolved = lookupFacetID(snap.Terms, term)
						_ = cacheFacets(cmd.Context(), db.DB(), snap)
					}
				}
				if resolved != "" {
					termID = resolved
				} else {
					termID = ""
				}
			}
			subjectID := subject
			if subjectID != "" && !isAllDigits(subjectID) {
				resolved, _ := store.FindSubjectIDByName(cmd.Context(), db.DB(), subject)
				if resolved == "" {
					if snap, ferr := bsource.New(flags.timeout).FetchFacets(cmd.Context()); ferr == nil {
						resolved = lookupFacetID(snap.SubjectAreas, subject)
						_ = cacheFacets(cmd.Context(), db.DB(), snap)
					}
				}
				if resolved != "" {
					subjectID = resolved
				} else {
					subjectID = ""
				}
			}

			client := bsource.New(flags.timeout)
			cards := make([]bsource.SectionCard, 0, limit)
			for page := 0; page < maxScanPages && len(cards) < limit; page++ {
				res, err := client.FetchSearch(cmd.Context(), bsource.SearchParams{
					Keywords:  keywords,
					TermID:    termID,
					SubjectID: subjectID,
					OpenOnly:  openOnly,
					Page:      page,
				})
				if err != nil {
					return fmt.Errorf("fetch page %d: %w", page, err)
				}
				if len(res.Cards) == 0 {
					break
				}
				for _, c := range res.Cards {
					if len(cards) >= limit {
						break
					}
					cards = append(cards, c)
				}
				if !res.HasNextPage {
					break
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"keywords":  keywords,
					"term":      term,
					"subject":   subject,
					"open_only": openOnly,
					"count":     len(cards),
					"cards":     cards,
				})
			}

			if len(cards) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching sections found.")
				return nil
			}
			headers := []string{"CCN", "Course", "Type", "Sec", "Title", "Instructors", "Days", "Time", "Open"}
			tbl := make([][]string, 0, len(cards))
			for _, c := range cards {
				tbl = append(tbl, []string{
					fmt.Sprintf("%d", c.CCN),
					c.CourseCode, c.SectionType, c.SectionNumber,
					truncate(c.Title, 30), truncate(c.Instructors, 28),
					c.MeetingDays, c.MeetingTime,
					fmt.Sprintf("%d", c.OpenSeats),
				})
			}
			return flags.printTable(cmd, headers, tbl)
		},
	}
	cmd.Flags().StringVar(&keywords, "keywords", "", "Free-text keywords")
	cmd.Flags().StringVar(&term, "term", "", "Term name (e.g. 'Fall 2026') or id")
	cmd.Flags().StringVar(&subject, "subject", "", "Subject name (e.g. 'Computer Science') or id")
	cmd.Flags().BoolVar(&openOnly, "open-only", false, "Restrict to sections with open seats (server-side facet)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum cards to return")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 5, "Maximum pages to walk")
	return cmd
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func lookupFacetID(items []bsource.FacetItem, name string) string {
	lname := strings.ToLower(strings.TrimSpace(name))
	for _, it := range items {
		if strings.ToLower(it.Name) == lname {
			return it.ID
		}
	}
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Name), lname) {
			return it.ID
		}
	}
	return ""
}
