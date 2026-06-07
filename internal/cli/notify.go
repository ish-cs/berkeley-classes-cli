// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ish-cs/berkeley-classes-cli/internal/cliutil"
	"github.com/spf13/cobra"
)

const (
	defaultFromEmail = "berkeleyclasses <onboarding@resend.dev>"
	resendAPI        = "https://api.resend.com/emails"
)

// newNotifyCmd reads pending watch alerts from Supabase and emails the
// subscribed users via Resend. Idempotent per 24-hour window.
func newNotifyCmd(flags *rootFlags) *cobra.Command {
	var supabaseURL, supabaseKey, resendKey, fromEmail, baseURL string

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Email users whose watched sections have open seats.",
		Long: `notify queries Supabase for pending watch-subscription alerts
(open_seats > 0, not notified in 24h) and sends an email per subscription via
Resend. Marks each subscription notified on success.

Credentials are read from --url, --key, --resend-key flags, or from
SUPABASE_URL, SUPABASE_SERVICE_KEY, RESEND_API_KEY environment variables.

If RESEND_API_KEY is missing, notify exits cleanly without sending anything;
this keeps the daily cron green while you're still wiring email.`,
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
			if resendKey == "" {
				resendKey = os.Getenv("RESEND_API_KEY")
			}
			if fromEmail == "" {
				fromEmail = os.Getenv("NOTIFY_FROM_EMAIL")
				if fromEmail == "" {
					fromEmail = defaultFromEmail
				}
			}
			if baseURL == "" {
				baseURL = os.Getenv("NOTIFY_BASE_URL")
				if baseURL == "" {
					baseURL = "https://berkeleyclasses-web.vercel.app"
				}
			}
			if supabaseURL == "" || supabaseKey == "" {
				return usageErr(fmt.Errorf("missing SUPABASE_URL or SUPABASE_SERVICE_KEY"))
			}
			if resendKey == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "notify: RESEND_API_KEY not set — skipping (no emails sent)")
				return nil
			}

			httpc := &http.Client{Timeout: 30 * time.Second}

			pending, err := fetchPendingAlerts(httpc, supabaseURL, supabaseKey)
			if err != nil {
				return fmt.Errorf("fetch pending: %w", err)
			}
			if len(pending) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "notify: 0 pending alerts")
				return nil
			}

			sent := 0
			failed := 0
			for _, a := range pending {
				if err := sendWatchEmail(httpc, resendKey, fromEmail, baseURL, a); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "notify: email to %s for ccn %d failed: %v\n", a.UserEmail, a.CCN, err)
					failed++
					continue
				}
				if err := markNotified(httpc, supabaseURL, supabaseKey, a.SubID); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "notify: mark_watch_notified failed for sub %s: %v\n", a.SubID, err)
					// Still count as sent — email went out
				}
				sent++
			}
			fmt.Fprintf(cmd.OutOrStdout(), "notify: sent %d, failed %d (total pending: %d)\n", sent, failed, len(pending))
			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"sent":    sent,
					"failed":  failed,
					"pending": len(pending),
				})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&supabaseURL, "url", "", "Supabase project URL (or SUPABASE_URL env)")
	cmd.Flags().StringVar(&supabaseKey, "key", "", "Supabase service-role key (or SUPABASE_SERVICE_KEY env)")
	cmd.Flags().StringVar(&resendKey, "resend-key", "", "Resend API key (or RESEND_API_KEY env)")
	cmd.Flags().StringVar(&fromEmail, "from", "", "From email address (or NOTIFY_FROM_EMAIL env)")
	cmd.Flags().StringVar(&baseURL, "site", "", "Site base URL used in email links (or NOTIFY_BASE_URL env)")
	return cmd
}

type pendingAlert struct {
	SubID        string `json:"sub_id"`
	UserEmail    string `json:"user_email"`
	CCN          int    `json:"ccn"`
	CourseCode   string `json:"course_code"`
	SectionLabel string `json:"section_label"`
	Title        string `json:"title"`
	OpenSeats    int    `json:"open_seats"`
	MeetingDays  string `json:"meeting_days"`
	MeetingTime  string `json:"meeting_time"`
}

func fetchPendingAlerts(c *http.Client, url, key string) ([]pendingAlert, error) {
	req, err := http.NewRequest(http.MethodPost, url+"/rest/v1/rpc/get_pending_watch_notifications", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out []pendingAlert
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode: %w (body: %s)", err, string(body))
	}
	return out, nil
}

func markNotified(c *http.Client, url, key, subID string) error {
	body, _ := json.Marshal(map[string]string{"p_sub_id": subID})
	req, err := http.NewRequest(http.MethodPost, url+"/rest/v1/rpc/mark_watch_notified", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func sendWatchEmail(c *http.Client, key, from, site string, a pendingAlert) error {
	classURL := fmt.Sprintf("%s/class/%d", strings.TrimRight(site, "/"), a.CCN)
	unwatchURL := fmt.Sprintf("%s/watch", strings.TrimRight(site, "/"))
	subject := fmt.Sprintf("%s open: %d seat(s) in %s", strings.TrimSpace(a.CourseCode+" "+a.SectionLabel), a.OpenSeats, a.CourseCode)

	html := fmt.Sprintf(`<!doctype html>
<html><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#111;line-height:1.5;max-width:560px;margin:24px auto;padding:0 16px;">
  <h2 style="margin:0 0 12px 0;">%s is open</h2>
  <p style="margin:0 0 16px 0;color:#444;">
    <strong>%d seat(s)</strong> open in <strong>%s %s</strong>%s<br>
    <span style="color:#777;">CCN %d · %s · %s</span>
  </p>
  <p style="margin:0 0 24px 0;">
    <a href="%s" style="display:inline-block;background:#111;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px;">View section →</a>
  </p>
  <p style="margin:0;color:#999;font-size:13px;">
    Sent by berkeleyclasses.com. <a href="%s" style="color:#999;">Manage your watched sections</a>.
  </p>
</body></html>`,
		htmlEscape(a.CourseCode), a.OpenSeats, htmlEscape(a.CourseCode), htmlEscape(a.SectionLabel),
		ifThen(a.Title != "", " ("+htmlEscape(a.Title)+")"),
		a.CCN, htmlEscape(a.MeetingDays), htmlEscape(a.MeetingTime),
		classURL, unwatchURL,
	)

	payload := map[string]any{
		"from":    from,
		"to":      []string{a.UserEmail},
		"subject": subject,
		"html":    html,
		"tags":    []map[string]string{{"name": "kind", "value": "watch_open_seat"}},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, resendAPI, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend HTTP %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&#39;")
	return r.Replace(s)
}

func ifThen(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}
