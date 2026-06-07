// Copyright 2026 ish-cs. MIT License. See LICENSE.

// Package bsource is the hand-written HTTP client and HTML parser for the UC
// Berkeley class schedule site (https://classes.berkeley.edu). The site is
// Drupal + Solr with no public JSON API; every novel command in this CLI
// ultimately reads through this package.
//
// The name "bsource" intentionally avoids collision with the generator's
// internal/client package — that one wraps a generic JSON API contract.
package bsource

// SectionCard is one section as it appears in a search-result row.
// All fields are best-effort: the site omits fields for some sections
// (e.g. async classes have no meeting_time) and a parser miss must not
// crash the whole page.
type SectionCard struct {
	NodeID              string `json:"node_id,omitempty"`
	Slug                string `json:"slug,omitempty"`
	TermName            string `json:"term_name,omitempty"`
	CCN                 int    `json:"ccn,omitempty"`
	CourseCode          string `json:"course_code,omitempty"`    // e.g. "COMPSCI 61A"
	SectionNumber       string `json:"section_number,omitempty"` // e.g. "001"
	SectionType         string `json:"section_type,omitempty"`   // LEC / DIS / LAB
	SectionSubNum       string `json:"section_sub_number,omitempty"`
	Title               string `json:"title,omitempty"`
	Department          string `json:"department,omitempty"`
	DepartmentURL       string `json:"department_url,omitempty"`
	Instructors         string `json:"instructors,omitempty"`
	MeetingDates        string `json:"meeting_dates,omitempty"`
	MeetingDays         string `json:"meeting_days,omitempty"`
	MeetingTime         string `json:"meeting_time,omitempty"`
	Location            string `json:"location,omitempty"`
	LocationURL         string `json:"location_url,omitempty"`
	InstructionMode     string `json:"instruction_mode,omitempty"`
	TimeConflictAllowed bool   `json:"time_conflict_allowed,omitempty"`
	Units               string `json:"units,omitempty"`
	OpenSeats           int    `json:"open_seats"`
	OpenSeatsLabel      string `json:"open_seats_label,omitempty"`
	Description         string `json:"description,omitempty"`
}

// SearchResults is one search page worth of cards plus pagination meta.
type SearchResults struct {
	Cards       []SectionCard `json:"cards"`
	Page        int           `json:"page"`
	HasNextPage bool          `json:"has_next_page"`
	TotalLabel  string        `json:"total_label,omitempty"`
}

// SectionDetail is the richer enrollment view from /content/<slug>.
type SectionDetail struct {
	Card          SectionCard `json:"card"`
	Enrolled      int         `json:"enrolled"`
	Waitlisted    int         `json:"waitlisted"`
	Capacity      int         `json:"capacity"`
	WaitlistMax   int         `json:"waitlist_max"`
	ReserveDetail string      `json:"reserve_detail,omitempty"`
}

// FacetItem is one item under a facet sidebar (e.g. one term, one subject).
type FacetItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FacetSnapshot mirrors the discovery/facets-snapshot.json shape so a
// fresh fetch can replace a stale cached file.
type FacetSnapshot struct {
	Terms               []FacetItem `json:"terms"`
	SubjectAreas        []FacetItem `json:"subject_area"`
	CourseLevel         []FacetItem `json:"course_level"`
	CourseTypes         []FacetItem `json:"course_types"`
	MeetsDays           []FacetItem `json:"meets_days"`
	Units               []FacetItem `json:"units"`
	EnrollmentStatus    []FacetItem `json:"enrollment_status"`
	ModeInstruction     []FacetItem `json:"mode_instruction"`
	BreadthRequirements []FacetItem `json:"breadth_requirements"`
	CourseThreads       []FacetItem `json:"course_threads"`
}

// SearchParams controls a search page request. Empty fields are skipped
// in the query string.
type SearchParams struct {
	Keywords  string
	TermID    string
	SubjectID string
	OpenOnly  bool
	Page      int // 0-indexed; the site uses ?page=N
}
