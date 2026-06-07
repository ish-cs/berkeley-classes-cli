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
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

// newIngestMetaCmd pulls grade distributions, prereqs, and requirement
// designations from Berkeleytime's public GraphQL endpoint and upserts them
// into the Supabase course_meta table.
func newIngestMetaCmd(flags *rootFlags) *cobra.Command {
	var supabaseURL, supabaseKey string
	var concurrency int
	var limit int

	cmd := &cobra.Command{
		Use:   "ingest-meta",
		Short: "Fetch grade distributions + prereqs from Berkeleytime; upsert into course_meta.",
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

			codes, err := fetchDistinctCourseCodes(ctx, httpc, supabaseURL, supabaseKey)
			if err != nil {
				return fmt.Errorf("listing course_codes: %w", err)
			}
			if limit > 0 && len(codes) > limit {
				codes = codes[:limit]
			}
			fmt.Fprintf(cmd.OutOrStdout(), "fetching meta for %d courses (concurrency=%d)\n", len(codes), concurrency)

			results := make(chan map[string]any, 64)
			var hits, misses, errs int64

			var wg sync.WaitGroup
			sem := make(chan struct{}, concurrency)
			for _, code := range codes {
				wg.Add(1)
				sem <- struct{}{}
				go func(courseCode string) {
					defer wg.Done()
					defer func() { <-sem }()
					row, found, err := fetchCourseMeta(ctx, httpc, courseCode)
					if err != nil {
						atomic.AddInt64(&errs, 1)
						return
					}
					if !found {
						atomic.AddInt64(&misses, 1)
						return
					}
					atomic.AddInt64(&hits, 1)
					results <- row
				}(code)
			}
			go func() { wg.Wait(); close(results) }()

			// Drain → batched upsert
			batch := make([]map[string]any, 0, 100)
			flush := func() error {
				if len(batch) == 0 {
					return nil
				}
				if err := upsertCourseMeta(ctx, httpc, supabaseURL, supabaseKey, batch); err != nil {
					return err
				}
				batch = batch[:0]
				return nil
			}
			for row := range results {
				batch = append(batch, row)
				if len(batch) >= 100 {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			if err := flush(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "ingest-meta complete: %d hits, %d misses, %d errors\n", hits, misses, errs)
			return nil
		},
	}

	cmd.Flags().StringVar(&supabaseURL, "url", "", "Supabase project URL (or SUPABASE_URL env)")
	cmd.Flags().StringVar(&supabaseKey, "key", "", "Supabase service-role key (or SUPABASE_SERVICE_KEY env)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Concurrent Berkeleytime requests")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit number of courses processed (0 = all)")
	return cmd
}

var courseCodeRe = regexp.MustCompile(`^([A-Z&/ ]+?)\s+([0-9A-Z]+)$`)

func splitCourseCode(code string) (subject, number string, ok bool) {
	m := courseCodeRe.FindStringSubmatch(strings.TrimSpace(code))
	if m == nil {
		return "", "", false
	}
	return strings.ReplaceAll(m[1], " ", ""), m[2], true
}

type btGradeEntry struct {
	Letter     string  `json:"letter"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type btCourse struct {
	Title              string `json:"title"`
	Requirements       *string `json:"requirements"`
	RequiredCourses    []struct {
		Subject string `json:"subject"`
		Number  string `json:"number"`
	} `json:"requiredCourses"`
	GradeDistribution *struct {
		Average      float64        `json:"average"`
		Distribution []btGradeEntry `json:"distribution"`
	} `json:"gradeDistribution"`
	MostRecentClass *struct {
		RequirementDesignation *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"requirementDesignation"`
	} `json:"mostRecentClass"`
}

const btGraphQLEndpoint = "https://berkeleytime.com/api/graphql"

const btQuery = `query Q($subject: String!, $number: CourseNumber!) {
  course(subject: $subject, number: $number) {
    title
    requirements
    requiredCourses { subject number }
    gradeDistribution { average distribution { letter count percentage } }
    mostRecentClass { requirementDesignation { code description } }
  }
}`

func fetchCourseMeta(ctx context.Context, httpc *http.Client, courseCode string) (map[string]any, bool, error) {
	subject, number, ok := splitCourseCode(courseCode)
	if !ok {
		return nil, false, nil
	}
	body, err := json.Marshal(map[string]any{
		"query": btQuery,
		"variables": map[string]string{
			"subject": subject,
			"number":  number,
		},
	})
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, btGraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://berkeleytime.com")
	req.Header.Set("User-Agent", "berkeley-classes-cli/ingest-meta")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// Berkeleytime returns 200 even for unknown courses; non-200 is a real error.
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, false, fmt.Errorf("berkeleytime HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var env struct {
		Data struct {
			Course *btCourse `json:"course"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, false, err
	}
	if env.Data.Course == nil {
		return nil, false, nil
	}
	c := env.Data.Course

	row := map[string]any{
		"course_code": courseCode,
		"updated_at":  time.Now().UTC().Format(time.RFC3339),
	}

	if c.GradeDistribution != nil && len(c.GradeDistribution.Distribution) > 0 {
		var sample int
		for _, d := range c.GradeDistribution.Distribution {
			sample += d.Count
		}
		row["grade_average"] = c.GradeDistribution.Average
		row["grade_sample_size"] = sample
		row["grade_distribution"] = c.GradeDistribution.Distribution
	} else {
		row["grade_average"] = nil
		row["grade_sample_size"] = nil
		row["grade_distribution"] = nil
	}

	if c.Requirements != nil && strings.TrimSpace(*c.Requirements) != "" {
		row["prereq_text"] = strings.TrimSpace(*c.Requirements)
	} else {
		row["prereq_text"] = nil
	}

	if len(c.RequiredCourses) > 0 {
		row["required_courses"] = c.RequiredCourses
	} else {
		row["required_courses"] = nil
	}

	if c.MostRecentClass != nil && c.MostRecentClass.RequirementDesignation != nil {
		row["requirement_code"] = c.MostRecentClass.RequirementDesignation.Code
		row["requirement_description"] = c.MostRecentClass.RequirementDesignation.Description
	} else {
		row["requirement_code"] = nil
		row["requirement_description"] = nil
	}

	return row, true, nil
}

func fetchDistinctCourseCodes(ctx context.Context, httpc *http.Client, baseURL, key string) ([]string, error) {
	// PostgREST returns distinct rows when ordering only matters by the field; we ask for course_code and dedupe in Go.
	endpoint := strings.TrimRight(baseURL, "/") + "/rest/v1/sections?select=course_code&course_code=not.is.null"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Range-Unit", "items")
	// Pull all rows in pages of 5000.
	out := make([]string, 0, 4096)
	seen := make(map[string]struct{}, 4096)
	const pageSize = 5000
	for offset := 0; ; offset += pageSize {
		pagedURL, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := pagedURL.Query()
		q.Set("offset", fmt.Sprintf("%d", offset))
		q.Set("limit", fmt.Sprintf("%d", pageSize))
		pagedURL.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pagedURL.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("apikey", key)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		resp, err := httpc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("sections list HTTP %d: %s", resp.StatusCode, string(body))
		}
		var rows []struct {
			CourseCode string `json:"course_code"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			c := strings.TrimSpace(r.CourseCode)
			if c == "" {
				continue
			}
			if _, dup := seen[c]; dup {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
		if len(rows) < pageSize {
			break
		}
	}
	return out, nil
}

func upsertCourseMeta(ctx context.Context, httpc *http.Client, baseURL, key string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
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
