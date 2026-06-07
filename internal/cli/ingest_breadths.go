// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newIngestBreadthsCmd walks the classes.berkeley.edu requirement facet for a
// given term and writes the per-course breadth tags into course_meta.requirements.
//
// Source: classes.berkeley.edu Drupal facet `breadth_requirements`. Each
// requirement's filtered search page lists every section that satisfies it for
// the chosen term; we collapse those to course codes and dedupe.
func newIngestBreadthsCmd(flags *rootFlags) *cobra.Command {
	var supabaseURL, supabaseKey string
	var termID string

	cmd := &cobra.Command{
		Use:   "ingest-breadths",
		Short: "Scrape classes.berkeley.edu breadth requirements and upsert into course_meta.requirements.",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if supabaseURL == "" {
				supabaseURL = os.Getenv("SUPABASE_URL")
			}
			if supabaseKey == "" {
				supabaseKey = os.Getenv("SUPABASE_SERVICE_KEY")
			}
			if supabaseURL == "" {
				return usageErr(fmt.Errorf("missing --url or SUPABASE_URL"))
			}
			if supabaseKey == "" {
				return usageErr(fmt.Errorf("missing --key or SUPABASE_SERVICE_KEY"))
			}

			httpc := &http.Client{Timeout: 30 * time.Second}
			ctx := cmd.Context()

			courseToReqs := map[string]map[string]struct{}{}
			for _, req := range breadthRequirements {
				codes, err := fetchCoursesForRequirement(ctx, httpc, req, termID)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %q failed: %v\n", req, err)
					continue
				}
				for c := range codes {
					if _, ok := courseToReqs[c]; !ok {
						courseToReqs[c] = map[string]struct{}{}
					}
					courseToReqs[c][req] = struct{}{}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d courses\n", req, len(codes))
			}

			rows := make([]map[string]any, 0, len(courseToReqs))
			for code, set := range courseToReqs {
				reqs := make([]string, 0, len(set))
				for r := range set {
					reqs = append(reqs, r)
				}
				rows = append(rows, map[string]any{
					"course_code":  code,
					"requirements": reqs,
				})
			}

			const batchSize = 200
			for i := 0; i < len(rows); i += batchSize {
				end := i + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				if err := upsertBreadths(ctx, httpc, supabaseURL, supabaseKey, rows[i:end]); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ingest-breadths complete: %d courses tagged\n", len(rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&supabaseURL, "url", "", "Supabase project URL (or SUPABASE_URL env)")
	cmd.Flags().StringVar(&supabaseKey, "key", "", "Supabase service-role key (or SUPABASE_SERVICE_KEY env)")
	cmd.Flags().StringVar(&termID, "term-id", "8588", "Classes.berkeley.edu term id (8588 = Fall 2026)")
	return cmd
}

var breadthRequirements = []string{
	"American Cultures",
	"American Hist & Institutions",
	"Arts & Literature",
	"Biological Science",
	"Entry Level Writing",
	"Historical Studies",
	"International Studies",
	"Philosophy & Values",
	"Physical Science",
	"Reading and Composition A",
	"Reading and Composition B",
	"Social & Behavioral Sciences",
}

var sectionLinkRe = regexp.MustCompile(`href="/content/20\d{2}-(?:fall|spring|summer)-([a-z0-9&-]+)-([0-9a-z]+)-\d+-[a-z]+-\d+"`)

func fetchCoursesForRequirement(ctx context.Context, httpc *http.Client, req, termID string) (map[string]struct{}, error) {
	codes := map[string]struct{}{}
	zeroStreak := 0
	for page := 0; page < 60; page++ {
		u := fmt.Sprintf(
			"https://classes.berkeley.edu/search/class?f%%5B0%%5D=breadth_requirements%%3A%s&f%%5B1%%5D=term%%3A%s",
			url.QueryEscape(req), termID,
		)
		if page > 0 {
			u += fmt.Sprintf("&page=%d", page)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "berkeley-classes-cli/ingest-breadths")
		resp, err := httpc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		matches := sectionLinkRe.FindAllSubmatch(body, -1)
		if len(matches) == 0 {
			break
		}
		newCount := 0
		for _, m := range matches {
			subject := strings.ToUpper(strings.ReplaceAll(string(m[1]), "-", " "))
			number := strings.ToUpper(string(m[2]))
			code := subject + " " + number
			if _, ok := codes[code]; !ok {
				newCount++
				codes[code] = struct{}{}
			}
		}
		if newCount == 0 {
			zeroStreak++
			if zeroStreak >= 2 {
				break
			}
		} else {
			zeroStreak = 0
		}
	}
	return codes, nil
}

func upsertBreadths(ctx context.Context, httpc *http.Client, baseURL, key string, rows []map[string]any) error {
	body, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/rest/v1/course_meta"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("course_meta upsert HTTP %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}
