// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"fmt"
	"time"

	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelSinceCmd(flags *rootFlags) *cobra.Command {
	var flagTerm string
	var flagHours int
	var flagDelta int

	cmd := &cobra.Command{
		Use:         "since",
		Short:       "Surface new sections, cancellations, instructor swaps, and enrollment moves since the last sync.",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if flagHours <= 0 {
				flagHours = 24
			}
			if flagDelta <= 0 {
				flagDelta = 5
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			sinceUnix := time.Now().Add(-time.Duration(flagHours) * time.Hour).Unix()
			snaps, err := store.SnapshotsSince(cmd.Context(), db.DB(), sinceUnix)
			if err != nil {
				return err
			}

			// Build first/last per CCN within the window
			type pair struct {
				first store.SnapshotRow
				last  store.SnapshotRow
				has   bool
			}
			byCCN := make(map[int]*pair)
			for _, sn := range snaps {
				p, ok := byCCN[sn.CCN]
				if !ok {
					byCCN[sn.CCN] = &pair{first: sn, last: sn, has: true}
					continue
				}
				if sn.TakenAt > p.last.TakenAt {
					p.last = sn
				}
				if sn.TakenAt < p.first.TakenAt {
					p.first = sn
				}
			}

			type change struct {
				CCN              int    `json:"ccn"`
				CourseCode       string `json:"course_code,omitempty"`
				SectionType      string `json:"section_type,omitempty"`
				Title            string `json:"title,omitempty"`
				OpenSeatsBefore  int    `json:"open_seats_before"`
				OpenSeatsAfter   int    `json:"open_seats_after"`
				EnrolledBefore   int    `json:"enrolled_before"`
				EnrolledAfter    int    `json:"enrolled_after"`
				WaitlistedBefore int    `json:"waitlisted_before"`
				WaitlistedAfter  int    `json:"waitlisted_after"`
				DeltaOpen        int    `json:"delta_open"`
			}
			changes := make([]change, 0)

			for ccn, p := range byCCN {
				deltaOpen := p.last.OpenSeats - p.first.OpenSeats
				deltaEnr := p.last.Enrolled - p.first.Enrolled
				deltaWait := p.last.Waitlisted - p.first.Waitlisted
				if absInt(deltaOpen) < flagDelta && absInt(deltaEnr) < flagDelta && absInt(deltaWait) < flagDelta {
					continue
				}
				sec, _ := store.LookupSectionByCCN(cmd.Context(), db.DB(), ccn)
				ch := change{
					CCN:             ccn,
					OpenSeatsBefore: p.first.OpenSeats, OpenSeatsAfter: p.last.OpenSeats,
					EnrolledBefore: p.first.Enrolled, EnrolledAfter: p.last.Enrolled,
					WaitlistedBefore: p.first.Waitlisted, WaitlistedAfter: p.last.Waitlisted,
					DeltaOpen: deltaOpen,
				}
				if sec != nil {
					ch.CourseCode = sec.CourseCode
					ch.SectionType = sec.SectionType
					ch.Title = sec.Title
				}
				changes = append(changes, ch)
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"since_hours":     flagHours,
					"delta_threshold": flagDelta,
					"term":            flagTerm,
					"change_count":    len(changes),
					"changes":         changes,
					"snapshot_count":  len(snaps),
				})
			}
			if len(snaps) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(),
					"No snapshots in the last", flagHours, "hours. Run 'sync' to capture a baseline, then again later to compare.")
				return nil
			}
			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(),
					"No section changed by ≥", flagDelta, "seats in the last", flagHours, "hours.")
				return nil
			}
			headers := []string{"CCN", "Course", "Type", "ΔOpen", "Open(before→after)", "Enrolled(before→after)", "Waitlist(before→after)"}
			rows := make([][]string, 0, len(changes))
			for _, c := range changes {
				rows = append(rows, []string{
					fmt.Sprintf("%d", c.CCN), c.CourseCode, c.SectionType,
					fmt.Sprintf("%+d", c.DeltaOpen),
					fmt.Sprintf("%d→%d", c.OpenSeatsBefore, c.OpenSeatsAfter),
					fmt.Sprintf("%d→%d", c.EnrolledBefore, c.EnrolledAfter),
					fmt.Sprintf("%d→%d", c.WaitlistedBefore, c.WaitlistedAfter),
				})
			}
			return flags.printTable(cmd, headers, rows)
		},
	}
	cmd.Flags().StringVar(&flagTerm, "term", "", "Term name (informational only — filtering is per-CCN)")
	cmd.Flags().IntVar(&flagHours, "hours", 24, "Look-back window in hours")
	cmd.Flags().IntVar(&flagDelta, "delta", 5, "Minimum absolute seat-delta to report")
	return cmd
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
