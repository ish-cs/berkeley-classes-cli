// Copyright 2026 ish-cs. MIT License. See LICENSE.

package bsource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BaseURL is the canonical entry point for the public class schedule.
const BaseURL = "https://classes.berkeley.edu"

// UserAgent is the polite identifier sent on every request. Real browsers
// will see anything; we identify as the CLI to keep ops happy.
const UserAgent = "bcourses/0.1 (+https://github.com/ish-cs/bcourses)"

// Client is a thin wrapper around http.Client with a fixed base URL.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

// New returns a Client with a timeout-bounded http.Client.
func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		HTTP:    &http.Client{Timeout: timeout},
		BaseURL: BaseURL,
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (string, error) {
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return "", fmt.Errorf("bsource: parsing url %q: %w", path, err)
	}
	if query != nil {
		// We want repeated f[N]= keys, so encode manually
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("bsource: new request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("bsource: GET %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	// Rate-limit handling: surface 429 distinctly with a hint and respect
	// Retry-After when the upstream sets it. classes.berkeley.edu currently
	// has no documented rate limit, but this keeps polite-client behavior
	// once the site adds one.
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := resp.Header.Get("Retry-After")
		if retry == "" {
			retry = "unknown"
		}
		return "", fmt.Errorf("bsource: GET %s rate-limited (HTTP 429, Retry-After=%s); slow down or pass --rate-limit", u.String(), retry)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("bsource: reading body for %s: %w", u.String(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("bsource: GET %s returned HTTP %d", u.String(), resp.StatusCode)
	}
	return string(body), nil
}

// FetchSearch fetches one /search/class page and parses every card.
// page is 0-indexed; the site uses page=0..N (page=0 is implicit when omitted).
func (c *Client) FetchSearch(ctx context.Context, params SearchParams) (*SearchResults, error) {
	q := url.Values{}
	if params.Keywords != "" {
		q.Set("search", params.Keywords)
	}
	idx := 0
	if params.TermID != "" {
		q.Set(fmt.Sprintf("f[%d]", idx), "term:"+params.TermID)
		idx++
	}
	if params.SubjectID != "" {
		q.Set(fmt.Sprintf("f[%d]", idx), "subject_area:"+params.SubjectID)
		idx++
	}
	if params.OpenOnly {
		// The site exposes "Open Seats" under enrollment_status facet with
		// the value "open_seats". The exact value alias may vary across
		// site versions; this filter is best-effort.
		q.Set(fmt.Sprintf("f[%d]", idx), "enrollment_status:open_seats")
		idx++
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	body, err := c.get(ctx, "/search/class", q)
	if err != nil {
		return nil, err
	}
	res, err := ParseSearch(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bsource: parsing search results: %w", err)
	}
	res.Page = params.Page
	return res, nil
}

// FetchDetail fetches a single section detail page.
// slug is the part after /content/, e.g. "2026-fall-compsci-61a-001-lec-001".
func (c *Client) FetchDetail(ctx context.Context, slug string) (*SectionDetail, error) {
	slug = strings.TrimPrefix(slug, "/")
	slug = strings.TrimPrefix(slug, "content/")
	body, err := c.get(ctx, "/content/"+slug, nil)
	if err != nil {
		return nil, err
	}
	d, err := ParseDetail(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bsource: parsing detail: %w", err)
	}
	d.Card.Slug = slug
	return d, nil
}

// FetchFacets fetches the home / empty-search page and extracts every
// sidebar facet (terms, subjects, etc.).
func (c *Client) FetchFacets(ctx context.Context) (*FacetSnapshot, error) {
	q := url.Values{}
	// Empty f[0]=term: triggers the no-filter all-facets view; the
	// home page also exposes the same sidebar.
	body, err := c.get(ctx, "/search/class", q)
	if err != nil {
		return nil, err
	}
	return ParseFacets(strings.NewReader(body))
}
