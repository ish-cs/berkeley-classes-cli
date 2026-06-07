// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"strconv"

	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelConflictCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "conflict <CCN_A> <CCN_B>",
		Short:       "Check whether two CCNs conflict on day-of-week and time.",
		Example:     "  bcourses conflict 29147 32104",
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
				return usageErr(fmt.Errorf("conflict requires two CCNs"))
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

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
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
				return notFoundErr(fmt.Errorf("CCN %d not in local store — run 'bcourses sync --term <term> --subject <subj>' first", a))
			}
			if sb == nil {
				return notFoundErr(fmt.Errorf("CCN %d not in local store — run 'bcourses sync --term <term> --subject <subj>' first", b))
			}

			daysA := parseMeetingDays(sa.MeetingDays)
			daysB := parseMeetingDays(sb.MeetingDays)
			startA, endA, okA := parseMeetingTime(sa.MeetingTime)
			startB, endB, okB := parseMeetingTime(sb.MeetingTime)

			dayOverlap := intersectDays(daysA, daysB)
			timeOverlap := false
			if okA && okB && len(dayOverlap) > 0 {
				timeOverlap = startA < endB && startB < endA
			}
			conflict := timeOverlap

			type conflictResult struct {
				CCNA            int      `json:"ccn_a"`
				CCNB            int      `json:"ccn_b"`
				CourseA         string   `json:"course_a"`
				CourseB         string   `json:"course_b"`
				DaysA           []string `json:"days_a"`
				DaysB           []string `json:"days_b"`
				TimeA           string   `json:"time_a"`
				TimeB           string   `json:"time_b"`
				OverlappingDays []string `json:"overlapping_days"`
				TimeOverlap     bool     `json:"time_overlap"`
				Conflict        bool     `json:"conflict"`
				Reason          string   `json:"reason,omitempty"`
			}
			result := conflictResult{
				CCNA: a, CCNB: b,
				CourseA: fmt.Sprintf("%s %s %s", sa.CourseCode, sa.SectionType, sa.SectionNumber),
				CourseB: fmt.Sprintf("%s %s %s", sb.CourseCode, sb.SectionType, sb.SectionNumber),
				DaysA:   daysA, DaysB: daysB,
				TimeA: sa.MeetingTime, TimeB: sb.MeetingTime,
				OverlappingDays: dayOverlap,
				TimeOverlap:     timeOverlap,
				Conflict:        conflict,
			}
			if conflict {
				result.Reason = fmt.Sprintf("Overlap on %v during %s vs %s", dayOverlap, sa.MeetingTime, sb.MeetingTime)
			} else if len(dayOverlap) == 0 {
				result.Reason = "No overlapping days"
			} else if !okA || !okB {
				result.Reason = "One section has no scheduled time (async)"
			} else {
				result.Reason = "Overlapping days but non-overlapping times"
			}

			if flags.asJSON {
				return flags.printJSON(cmd, result)
			}
			label := "OK (no conflict)"
			if conflict {
				label = "CONFLICT"
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"%s\n%d %s [%v %s]\n%d %s [%v %s]\n%s\n",
				label,
				result.CCNA, result.CourseA, result.DaysA, result.TimeA,
				result.CCNB, result.CourseB, result.DaysB, result.TimeB,
				result.Reason,
			)
			return nil
		},
	}
	return cmd
}
