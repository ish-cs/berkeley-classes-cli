// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ish-cs/berkeley-classes-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelCompareCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "compare <CCN_A> <CCN_B>",
		Short:       "Side-by-side comparison of two sections' meeting times, enrollment, and instructors.",
		Example:     "  berkeley-classes compare 29147 32104\n  berkeley-classes compare 29147 32104 --json",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) < 2 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("compare requires two CCNs"))
			}
			a, err := strconv.Atoi(args[0])
			if err != nil || a <= 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid CCN %q (must be a positive integer)", args[0]))
			}
			b, err := strconv.Atoi(args[1])
			if err != nil || b <= 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid CCN %q (must be a positive integer)", args[1]))
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("berkeley-classes"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			sa, err := store.LookupSectionByCCN(cmd.Context(), db.DB(), a)
			if err != nil {
				return err
			}
			sb, err := store.LookupSectionByCCN(cmd.Context(), db.DB(), b)
			if err != nil {
				return err
			}
			if sa == nil {
				return notFoundErr(fmt.Errorf("CCN %d not in local store — run 'berkeley-classes sync --term <term> --subject <subj>' first", a))
			}
			if sb == nil {
				return notFoundErr(fmt.Errorf("CCN %d not in local store — run 'berkeley-classes sync --term <term> --subject <subj>' first", b))
			}

			daysA := parseMeetingDays(sa.MeetingDays)
			daysB := parseMeetingDays(sb.MeetingDays)
			startA, endA, okA := parseMeetingTime(sa.MeetingTime)
			startB, endB, okB := parseMeetingTime(sb.MeetingTime)
			overlapDays := intersectDays(daysA, daysB)
			conflict := okA && okB && len(overlapDays) > 0 && startA < endB && startB < endA

			result := compareResult{
				A:               sectionFacts(sa),
				B:               sectionFacts(sb),
				OverlappingDays: overlapDays,
				Conflict:        conflict,
			}

			if flags.asJSON {
				return flags.printJSON(cmd, result)
			}

			headers := []string{"Field", fmt.Sprintf("CCN %d", a), fmt.Sprintf("CCN %d", b)}
			rows := [][]string{
				{"Course", result.A.Course, result.B.Course},
				{"Title", truncate(result.A.Title, 40), truncate(result.B.Title, 40)},
				{"Instructors", truncate(result.A.Instructors, 40), truncate(result.B.Instructors, 40)},
				{"Days", strings.Join(result.A.Days, " "), strings.Join(result.B.Days, " ")},
				{"Time", result.A.MeetingTime, result.B.MeetingTime},
				{"Location", truncate(result.A.Location, 40), truncate(result.B.Location, 40)},
				{"Units", result.A.Units, result.B.Units},
				{"Open seats", fmt.Sprintf("%d", result.A.OpenSeats), fmt.Sprintf("%d", result.B.OpenSeats)},
				{"Enrolled", fmt.Sprintf("%d / %d", result.A.Enrolled, result.A.Capacity), fmt.Sprintf("%d / %d", result.B.Enrolled, result.B.Capacity)},
				{"Waitlisted", fmt.Sprintf("%d", result.A.Waitlisted), fmt.Sprintf("%d", result.B.Waitlisted)},
			}
			if err := flags.printTable(cmd, headers, rows); err != nil {
				return err
			}
			verdict := "OK (no conflict)"
			if conflict {
				verdict = fmt.Sprintf("CONFLICT on %v", overlapDays)
			} else if len(overlapDays) > 0 {
				verdict = fmt.Sprintf("Overlap on %v but times do not collide", overlapDays)
			}
			fmt.Fprintln(cmd.OutOrStdout(), verdict)
			return nil
		},
	}
	return cmd
}

type sectionFactsT struct {
	CCN         int      `json:"ccn"`
	Course      string   `json:"course"`
	Title       string   `json:"title"`
	Instructors string   `json:"instructors"`
	Days        []string `json:"days"`
	MeetingTime string   `json:"meeting_time"`
	Location    string   `json:"location"`
	Units       string   `json:"units"`
	OpenSeats   int      `json:"open_seats"`
	Enrolled    int      `json:"enrolled"`
	Capacity    int      `json:"capacity"`
	Waitlisted  int      `json:"waitlisted"`
}

type compareResult struct {
	A               sectionFactsT `json:"a"`
	B               sectionFactsT `json:"b"`
	OverlappingDays []string      `json:"overlapping_days"`
	Conflict        bool          `json:"conflict"`
}

func sectionFacts(s *store.BCSection) sectionFactsT {
	return sectionFactsT{
		CCN:         s.CCN,
		Course:      strings.TrimSpace(fmt.Sprintf("%s %s %s", s.CourseCode, s.SectionType, s.SectionNumber)),
		Title:       s.Title,
		Instructors: s.Instructors,
		Days:        parseMeetingDays(s.MeetingDays),
		MeetingTime: s.MeetingTime,
		Location:    s.Location,
		Units:       s.Units,
		OpenSeats:   s.OpenSeats,
		Enrolled:    s.Enrolled,
		Capacity:    s.Capacity,
		Waitlisted:  s.Waitlisted,
	}
}
