// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ish-cs/berkeley-classes-cli/internal/cliutil"
	"github.com/ish-cs/berkeley-classes-cli/internal/store"
	"github.com/spf13/cobra"
)

const pushBatchSize = 500

// newPushCmd uploads the local SQLite store (bc_terms, bc_subjects,
// bc_sections) to a Supabase Postgres instance via PostgREST. Used by the
// berkeleyclasses.com web app — the CLI is the ingestion engine; this command
// is the bridge.
func newPushCmd(flags *rootFlags) *cobra.Command {
	var supabaseURL, supabaseKey string
	var withSnapshots bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Upload the local store to a Supabase Postgres (terms, subjects, sections).",
		Long: `push uploads every row from the local SQLite store (bc_terms,
bc_subjects, bc_sections) to a Supabase Postgres via PostgREST upserts.

Credentials are read from --url and --key flags, or from
SUPABASE_URL and SUPABASE_SERVICE_KEY environment variables (service role JWT
is required to bypass RLS on writes).`,
		Example: "  SUPABASE_URL=https://xxx.supabase.co SUPABASE_SERVICE_KEY=$KEY berkeley-classes push\n" +
			"  berkeley-classes push --url https://xxx.supabase.co --key $KEY --with-snapshots",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if cliutil.IsVerifyEnv() {
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

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("berkeley-classes"))
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()
			if err := store.EnsureBerkeleyTables(cmd.Context(), db.DB()); err != nil {
				return err
			}

			pc := &pushClient{
				baseURL: supabaseURL,
				key:     supabaseKey,
				http:    &http.Client{Timeout: 60 * time.Second},
				stderr:  cmd.ErrOrStderr(),
			}

			ctx := cmd.Context()
			termRows, err := readAllTerms(ctx, db.DB())
			if err != nil {
				return fmt.Errorf("reading terms: %w", err)
			}
			if err := pc.upsert(ctx, "terms", termRows); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pushed %d terms\n", len(termRows))

			subjectRows, err := readAllSubjects(ctx, db.DB())
			if err != nil {
				return fmt.Errorf("reading subjects: %w", err)
			}
			if err := pc.upsert(ctx, "subjects", subjectRows); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pushed %d subjects\n", len(subjectRows))

			sectionRows, err := readAllSections(ctx, db.DB())
			if err != nil {
				return fmt.Errorf("reading sections: %w", err)
			}
			if err := pc.upsertBatched(ctx, "sections", sectionRows, pushBatchSize); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pushed %d sections\n", len(sectionRows))

			if withSnapshots {
				snapRows, err := readRecentSnapshots(ctx, db.DB())
				if err != nil {
					return fmt.Errorf("reading snapshots: %w", err)
				}
				if err := pc.insertBatched(ctx, "section_snapshots", snapRows, pushBatchSize); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "pushed %d snapshots\n", len(snapRows))
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"terms":    len(termRows),
					"subjects": len(subjectRows),
					"sections": len(sectionRows),
				})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "push complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&supabaseURL, "url", "", "Supabase project URL (or SUPABASE_URL env)")
	cmd.Flags().StringVar(&supabaseKey, "key", "", "Supabase service-role key (or SUPABASE_SERVICE_KEY env)")
	cmd.Flags().BoolVar(&withSnapshots, "with-snapshots", false, "Also push recent enrollment snapshots")
	return cmd
}

type pushClient struct {
	baseURL string
	key     string
	http    *http.Client
	stderr  io.Writer
}

func (p *pushClient) postgrest(method, table string, body []byte, prefer string) error {
	url := p.baseURL + "/rest/v1/" + table
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", p.key)
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", prefer)

	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("postgrest %s %s: HTTP %d: %s", method, table, resp.StatusCode, string(buf))
	}
	return nil
}

func (p *pushClient) upsert(ctx context.Context, table string, rows any) error {
	if isEmpty(rows) {
		return nil
	}
	buf, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return p.postgrest(http.MethodPost, table, buf, "resolution=merge-duplicates,return=minimal")
}

func (p *pushClient) upsertBatched(ctx context.Context, table string, rows []map[string]any, batchSize int) error {
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := p.upsert(ctx, table, rows[start:end]); err != nil {
			return fmt.Errorf("batch %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func (p *pushClient) insertBatched(ctx context.Context, table string, rows []map[string]any, batchSize int) error {
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		buf, err := json.Marshal(rows[start:end])
		if err != nil {
			return err
		}
		if err := p.postgrest(http.MethodPost, table, buf, "return=minimal"); err != nil {
			return fmt.Errorf("batch %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func isEmpty(rows any) bool {
	switch v := rows.(type) {
	case []map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
}

func readAllTerms(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT term_id, name, COALESCE(kind,'') FROM bc_terms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, kind string
		if err := rows.Scan(&id, &name, &kind); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"term_id": id, "name": name, "kind": kind})
	}
	return out, rows.Err()
}

func readAllSubjects(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT subject_id, name, COALESCE(code,'') FROM bc_subjects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, code string
		if err := rows.Scan(&id, &name, &code); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"subject_id": id, "name": name, "code": code})
	}
	return out, rows.Err()
}

func readAllSections(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	rows, err := store.QuerySections(ctx, db, store.QuerySectionsOpts{Limit: 1_000_000})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"ccn":              r.CCN,
			"term_id":          nullableString(r.TermID),
			"subject_name":     nullableString(r.SubjectName),
			"course_code":      nullableString(r.CourseCode),
			"course_number":    nullableString(r.CourseNumber),
			"section_number":   nullableString(r.SectionNumber),
			"section_type":     nullableString(r.SectionType),
			"title":            nullableString(r.Title),
			"instructors":      nullableString(r.Instructors),
			"units":            nullableString(r.Units),
			"instruction_mode": nullableString(r.InstructionMode),
			"meeting_dates":    nullableString(r.MeetingDates),
			"meeting_days":     nullableString(r.MeetingDays),
			"meeting_time":     nullableString(r.MeetingTime),
			"location":         nullableString(r.Location),
			"slug":             nullableString(r.Slug),
			"description":      nullableString(r.Description),
			"open_seats":       r.OpenSeats,
			"enrolled":         r.Enrolled,
			"waitlisted":       r.Waitlisted,
			"capacity":         r.Capacity,
			"last_synced":      r.LastSynced,
		})
	}
	return out, nil
}

func readRecentSnapshots(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	rows, err := db.QueryContext(ctx,
		`SELECT ccn, taken_at, open_seats, enrolled, waitlisted, capacity
		 FROM bc_section_snapshots WHERE taken_at >= ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var ccn int
		var takenAt int64
		var open, enrolled, waitlisted, capacity int
		if err := rows.Scan(&ccn, &takenAt, &open, &enrolled, &waitlisted, &capacity); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"ccn":         ccn,
			"taken_at":    takenAt,
			"open_seats":  open,
			"enrolled":    enrolled,
			"waitlisted":  waitlisted,
			"capacity":    capacity,
		})
	}
	return out, rows.Err()
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
