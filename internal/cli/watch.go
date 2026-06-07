// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/ish-cs/bcourses-cli/internal/bsource"
	"github.com/ish-cs/bcourses-cli/internal/cliutil"
	"github.com/ish-cs/bcourses-cli/internal/store"
	"github.com/spf13/cobra"
)

func newNovelWatchCmd(flags *rootFlags) *cobra.Command {
	var flagInterval time.Duration
	var flagMaxChecks int

	cmd := &cobra.Command{
		Use:         "watch <CCN>",
		Short:       "Watch a CCN and report when open seats appear, waitlist shrinks, or capacity changes.",
		Example:     "  bcourses watch 29147 --interval 5m\n  bcourses watch 29202 --interval 10m --max-checks 12",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if cliutil.IsVerifyEnv() {
				return nil
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("watch requires a CCN"))
			}
			ccn, err := strconv.Atoi(args[0])
			if err != nil || ccn <= 0 {
				return usageErr(fmt.Errorf("invalid CCN %q", args[0]))
			}
			if flagInterval <= 0 {
				flagInterval = 5 * time.Minute
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("bcourses"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			cur, err := store.LookupSectionByCCN(cmd.Context(), db.DB(), ccn)
			if err != nil {
				return err
			}
			slug := ""
			if cur != nil {
				slug = cur.Slug
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			client := bsource.New(flags.timeout)

			lastOpen := -1
			lastEnrolled := -1
			lastWaitlisted := -1
			lastCapacity := -1
			if cur != nil {
				lastOpen = cur.OpenSeats
				lastEnrolled = cur.Enrolled
				lastWaitlisted = cur.Waitlisted
				lastCapacity = cur.Capacity
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watching CCN %d every %s (Ctrl-C to stop)\n", ccn, flagInterval)

			checks := 0
			for {
				if flagMaxChecks > 0 && checks >= flagMaxChecks {
					return nil
				}
				checks++

				// Resolve slug if we don't have one yet (search by CCN as keyword)
				if slug == "" {
					res, sErr := client.FetchSearch(ctx, bsource.SearchParams{Keywords: strconv.Itoa(ccn), Page: 0})
					if sErr == nil {
						for _, c := range res.Cards {
							if c.CCN == ccn {
								slug = c.Slug
								break
							}
						}
					}
				}

				var openSeats, enrolled, waitlisted, capacity int
				if slug != "" {
					det, dErr := client.FetchDetail(ctx, slug)
					if dErr != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "%s  fetch error: %v\n",
							time.Now().Format(time.RFC3339), dErr)
					} else {
						openSeats = det.Card.OpenSeats
						enrolled = det.Enrolled
						waitlisted = det.Waitlisted
						capacity = det.Capacity
					}
				} else {
					// Fall back to a search page if we couldn't resolve a slug
					res, sErr := client.FetchSearch(ctx, bsource.SearchParams{Keywords: strconv.Itoa(ccn), Page: 0})
					if sErr != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "%s  fetch error: %v\n", time.Now().Format(time.RFC3339), sErr)
					} else {
						for _, c := range res.Cards {
							if c.CCN == ccn {
								openSeats = c.OpenSeats
								break
							}
						}
					}
				}

				if err := store.SnapshotEnrollment(ctx, db.DB(), ccn, openSeats, enrolled, waitlisted, capacity); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "snapshot error: %v\n", err)
				}

				if lastOpen == -1 || openSeats != lastOpen || enrolled != lastEnrolled || waitlisted != lastWaitlisted || capacity != lastCapacity {
					fmt.Fprintf(cmd.OutOrStdout(),
						"%s  CCN %d  open=%d enrolled=%d waitlist=%d capacity=%d\n",
						time.Now().Format(time.RFC3339), ccn, openSeats, enrolled, waitlisted, capacity)
				}
				lastOpen = openSeats
				lastEnrolled = enrolled
				lastWaitlisted = waitlisted
				lastCapacity = capacity

				// Short-circuit after the final poll so --max-checks=1 returns
				// immediately instead of sleeping for one more interval.
				if flagMaxChecks > 0 && checks >= flagMaxChecks {
					return nil
				}

				select {
				case <-ctx.Done():
					if errors.Is(ctx.Err(), context.Canceled) {
						return nil
					}
					return nil
				case <-time.After(flagInterval):
				}
			}
		},
	}
	cmd.Flags().DurationVar(&flagInterval, "interval", 5*time.Minute, "Poll interval")
	cmd.Flags().IntVar(&flagMaxChecks, "max-checks", 0, "Stop after N polls (0 = infinite)")
	return cmd
}
