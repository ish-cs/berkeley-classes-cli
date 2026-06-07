// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/ish-cs/bcourses-cli/internal/bsource"
	"github.com/ish-cs/bcourses-cli/internal/cliutil"
	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

// newSyncRealCmd is the hand-written replacement for the generator's no-op
// sync command. It walks /search/class for the chosen term/subject and
// upserts every section card into the local store, plus an enrollment
// snapshot per card for since/watch.
func newSyncRealCmd(flags *rootFlags) *cobra.Command {
	var term, subject, keywords string
	var maxPages int
	var refreshFacets bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the local store from classes.berkeley.edu (paginated search walk).",
		Annotations: map[string]string{
			"mcp:read-only": "false",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if cliutil.IsVerifyEnv() {
				return nil
			}
			if maxPages <= 0 {
				maxPages = 50
			}
			if cliutil.IsDogfoodEnv() && maxPages > 2 {
				maxPages = 2
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			client := bsource.New(flags.timeout)
			if refreshFacets {
				snap, ferr := client.FetchFacets(cmd.Context())
				if ferr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "facet refresh failed: %v\n", ferr)
				} else {
					_ = cacheFacets(cmd.Context(), db.DB(), snap)
				}
			}

			// Resolve names → ids
			termID := term
			if termID != "" && !isAllDigits(termID) {
				id, _ := store.FindTermIDByName(cmd.Context(), db.DB(), term)
				if id == "" {
					snap, ferr := client.FetchFacets(cmd.Context())
					if ferr == nil {
						_ = cacheFacets(cmd.Context(), db.DB(), snap)
						id = lookupFacetID(snap.Terms, term)
					}
				}
				if id == "" {
					return notFoundErr(fmt.Errorf("term %q not found (run 'terms --refresh' to refresh)", term))
				}
				termID = id
			}
			subjectID := subject
			if subjectID != "" && !isAllDigits(subjectID) {
				id, _ := store.FindSubjectIDByName(cmd.Context(), db.DB(), subject)
				if id == "" {
					snap, ferr := client.FetchFacets(cmd.Context())
					if ferr == nil {
						_ = cacheFacets(cmd.Context(), db.DB(), snap)
						id = lookupFacetID(snap.SubjectAreas, subject)
					}
				}
				if id == "" {
					return notFoundErr(fmt.Errorf("subject %q not found (run 'subjects --refresh' to refresh)", subject))
				}
				subjectID = id
			}

			synced := 0
			fmt.Fprintf(cmd.OutOrStdout(), "sync: term=%q subject=%q maxPages=%d\n", term, subject, maxPages)
			for page := 0; page < maxPages; page++ {
				res, err := client.FetchSearch(cmd.Context(), bsource.SearchParams{
					Keywords:  keywords,
					TermID:    termID,
					SubjectID: subjectID,
					Page:      page,
				})
				if err != nil {
					return fmt.Errorf("sync page %d: %w", page, err)
				}
				if len(res.Cards) == 0 {
					break
				}
				for _, c := range res.Cards {
					if c.CCN == 0 {
						continue
					}
					sec := store.BCSection{
						CCN:             c.CCN,
						TermID:          termID,
						SubjectName:     c.Department,
						CourseCode:      c.CourseCode,
						CourseNumber:    extractCourseNumber(c.CourseCode),
						SectionNumber:   c.SectionNumber,
						SectionType:     c.SectionType,
						Title:           c.Title,
						Instructors:     c.Instructors,
						Units:           c.Units,
						InstructionMode: c.InstructionMode,
						MeetingDates:    c.MeetingDates,
						MeetingDays:     c.MeetingDays,
						MeetingTime:     c.MeetingTime,
						Location:        c.Location,
						Slug:            c.Slug,
						Description:     c.Description,
						OpenSeats:       c.OpenSeats,
					}
					if err := store.UpsertSection(cmd.Context(), db.DB(), sec); err != nil {
						return err
					}
					if err := store.SnapshotEnrollment(cmd.Context(), db.DB(), c.CCN, c.OpenSeats, 0, 0, 0); err != nil {
						return err
					}
					synced++
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  page %d: %d cards (total synced: %d)\n", page, len(res.Cards), synced)
				if !res.HasNextPage {
					break
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"term":            term,
					"subject":         subject,
					"keywords":        keywords,
					"sections_synced": synced,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sync complete: %d sections synced\n", synced)
			return nil
		},
	}
	cmd.Flags().StringVar(&term, "term", "", "Term name (e.g. 'Fall 2026') or id")
	cmd.Flags().StringVar(&subject, "subject", "", "Subject name (e.g. 'Computer Science') or id")
	cmd.Flags().StringVar(&keywords, "keywords", "", "Optional keyword filter")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "Maximum pages to walk")
	cmd.Flags().BoolVar(&refreshFacets, "refresh-facets", false, "Refresh term/subject facet cache before syncing")
	return cmd
}

// extractCourseNumber turns "COMPSCI 61A" into "61A".
func extractCourseNumber(courseCode string) string {
	idx := strings.LastIndex(courseCode, " ")
	if idx < 0 {
		return courseCode
	}
	return strings.TrimSpace(courseCode[idx+1:])
}
