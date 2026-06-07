// Copyright 2026 ish-cs. MIT License. See LICENSE.

// Hand-written tables for the Berkeley class schedule novel commands. These
// live outside the generator's migrate path on purpose: the generator
// regenerates store.go from a spec but never touches files with this naming
// convention.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// berkeleyMigrations are idempotent (IF NOT EXISTS) so the function can be
// called from every command's RunE prologue without ordering concerns.
var berkeleyMigrations = []string{
	`CREATE TABLE IF NOT EXISTS bc_terms (
		term_id TEXT PRIMARY KEY,
		name    TEXT NOT NULL,
		kind    TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS bc_subjects (
		subject_id TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		code       TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS bc_sections (
		ccn              INTEGER PRIMARY KEY,
		term_id          TEXT,
		subject_name     TEXT,
		course_code      TEXT,
		course_number    TEXT,
		section_number   TEXT,
		section_type     TEXT,
		title            TEXT,
		instructors      TEXT,
		units            TEXT,
		instruction_mode TEXT,
		meeting_dates    TEXT,
		meeting_days     TEXT,
		meeting_time     TEXT,
		location         TEXT,
		slug             TEXT,
		description      TEXT,
		open_seats       INTEGER DEFAULT 0,
		enrolled         INTEGER DEFAULT 0,
		waitlisted       INTEGER DEFAULT 0,
		capacity         INTEGER DEFAULT 0,
		last_synced      INTEGER DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS bc_sections_course_code ON bc_sections(course_code)`,
	`CREATE INDEX IF NOT EXISTS bc_sections_term ON bc_sections(term_id)`,
	`CREATE INDEX IF NOT EXISTS bc_sections_instructors ON bc_sections(instructors)`,
	`CREATE TABLE IF NOT EXISTS bc_section_snapshots (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		ccn        INTEGER,
		taken_at   INTEGER,
		open_seats INTEGER,
		enrolled   INTEGER,
		waitlisted INTEGER,
		capacity   INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS bc_snapshot_ccn_taken ON bc_section_snapshots(ccn, taken_at)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS bc_sections_fts USING fts5(
		title, instructors, course_code, description,
		content='bc_sections', content_rowid='ccn',
		tokenize='porter unicode61'
	)`,
}

// EnsureBerkeleyTables runs all berkeley table migrations against db. Safe to
// call from any RunE prologue; the IF NOT EXISTS clauses make repeated calls
// a no-op.
func EnsureBerkeleyTables(ctx context.Context, db *sql.DB) error {
	for _, m := range berkeleyMigrations {
		if _, err := db.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("bc migration failed: %w (sql=%q)", err, firstLine(m))
		}
	}
	return nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// BCSection is the row shape returned by QuerySections and friends.
type BCSection struct {
	CCN             int    `json:"ccn"`
	TermID          string `json:"term_id,omitempty"`
	SubjectName     string `json:"subject_name,omitempty"`
	CourseCode      string `json:"course_code,omitempty"`
	CourseNumber    string `json:"course_number,omitempty"`
	SectionNumber   string `json:"section_number,omitempty"`
	SectionType     string `json:"section_type,omitempty"`
	Title           string `json:"title,omitempty"`
	Instructors     string `json:"instructors,omitempty"`
	Units           string `json:"units,omitempty"`
	InstructionMode string `json:"instruction_mode,omitempty"`
	MeetingDates    string `json:"meeting_dates,omitempty"`
	MeetingDays     string `json:"meeting_days,omitempty"`
	MeetingTime     string `json:"meeting_time,omitempty"`
	Location        string `json:"location,omitempty"`
	Slug            string `json:"slug,omitempty"`
	Description     string `json:"description,omitempty"`
	OpenSeats       int    `json:"open_seats"`
	Enrolled        int    `json:"enrolled"`
	Waitlisted      int    `json:"waitlisted"`
	Capacity        int    `json:"capacity"`
	LastSynced      int64  `json:"last_synced,omitempty"`
}

// UpsertSection inserts or replaces one row in bc_sections. Returns no error
// if ccn==0; the caller should pre-validate.
func UpsertSection(ctx context.Context, db *sql.DB, s BCSection) error {
	if s.CCN == 0 {
		return fmt.Errorf("UpsertSection: ccn==0")
	}
	if s.LastSynced == 0 {
		s.LastSynced = time.Now().Unix()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO bc_sections (
			ccn, term_id, subject_name, course_code, course_number, section_number, section_type,
			title, instructors, units, instruction_mode,
			meeting_dates, meeting_days, meeting_time, location, slug, description,
			open_seats, enrolled, waitlisted, capacity, last_synced
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(ccn) DO UPDATE SET
			term_id=excluded.term_id,
			subject_name=excluded.subject_name,
			course_code=excluded.course_code,
			course_number=excluded.course_number,
			section_number=excluded.section_number,
			section_type=excluded.section_type,
			title=excluded.title,
			instructors=excluded.instructors,
			units=excluded.units,
			instruction_mode=excluded.instruction_mode,
			meeting_dates=excluded.meeting_dates,
			meeting_days=excluded.meeting_days,
			meeting_time=excluded.meeting_time,
			location=excluded.location,
			slug=excluded.slug,
			description=excluded.description,
			open_seats=excluded.open_seats,
			enrolled=excluded.enrolled,
			waitlisted=excluded.waitlisted,
			capacity=excluded.capacity,
			last_synced=excluded.last_synced
	`,
		s.CCN, s.TermID, s.SubjectName, s.CourseCode, s.CourseNumber, s.SectionNumber, s.SectionType,
		s.Title, s.Instructors, s.Units, s.InstructionMode,
		s.MeetingDates, s.MeetingDays, s.MeetingTime, s.Location, s.Slug, s.Description,
		s.OpenSeats, s.Enrolled, s.Waitlisted, s.Capacity, s.LastSynced,
	)
	if err != nil {
		return fmt.Errorf("upsert section %d: %w", s.CCN, err)
	}
	return nil
}

// UpsertTerm caches one term in bc_terms.
func UpsertTerm(ctx context.Context, db *sql.DB, id, name, kind string) error {
	if id == "" || name == "" {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO bc_terms (term_id, name, kind) VALUES (?,?,?)
		 ON CONFLICT(term_id) DO UPDATE SET name=excluded.name, kind=excluded.kind`,
		id, name, kind)
	return err
}

// UpsertSubject caches one subject in bc_subjects.
func UpsertSubject(ctx context.Context, db *sql.DB, id, name, code string) error {
	if id == "" || name == "" {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO bc_subjects (subject_id, name, code) VALUES (?,?,?)
		 ON CONFLICT(subject_id) DO UPDATE SET name=excluded.name, code=excluded.code`,
		id, name, code)
	return err
}

// SnapshotEnrollment writes one snapshot row to bc_section_snapshots.
func SnapshotEnrollment(ctx context.Context, db *sql.DB, ccn int, openSeats, enrolled, waitlisted, capacity int) error {
	if ccn == 0 {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO bc_section_snapshots (ccn, taken_at, open_seats, enrolled, waitlisted, capacity)
		 VALUES (?,?,?,?,?,?)`,
		ccn, time.Now().Unix(), openSeats, enrolled, waitlisted, capacity)
	return err
}

// QuerySections returns sections matching the optional filters. Empty
// filters match all rows. Pass courseCode like "COMPSCI 61A" — wildcard is
// applied automatically.
func QuerySections(ctx context.Context, db *sql.DB, opts QuerySectionsOpts) ([]BCSection, error) {
	where := []string{"1=1"}
	args := []any{}
	if opts.CourseCode != "" {
		where = append(where, "course_code = ?")
		args = append(args, opts.CourseCode)
	}
	if opts.CourseCodeLike != "" {
		where = append(where, "course_code LIKE ?")
		args = append(args, opts.CourseCodeLike)
	}
	if opts.TermID != "" {
		where = append(where, "term_id = ?")
		args = append(args, opts.TermID)
	}
	if opts.InstructorLike != "" {
		where = append(where, "instructors LIKE ?")
		args = append(args, opts.InstructorLike)
	}
	if opts.SubjectNameLike != "" {
		where = append(where, "LOWER(subject_name) LIKE LOWER(?)")
		args = append(args, opts.SubjectNameLike)
	}
	if opts.OpenOnly {
		where = append(where, "open_seats > 0")
	}
	if opts.CCN > 0 {
		where = append(where, "ccn = ?")
		args = append(args, opts.CCN)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}
	// #nosec G202 -- joinAnd composes only fixed internal predicate
	// strings (constant column = ? clauses) from the closed set above; no
	// caller input flows into the SQL text. All user values are bound via ?
	// placeholders in args[].
	q := `SELECT ccn, term_id, subject_name, course_code, course_number, section_number, section_type,
	             title, instructors, units, instruction_mode,
	             meeting_dates, meeting_days, meeting_time, location, slug, description,
	             open_seats, enrolled, waitlisted, capacity, last_synced
	      FROM bc_sections
	      WHERE ` + joinAnd(where) + `
	      ORDER BY course_code, section_number
	      LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query sections: %w", err)
	}
	defer rows.Close()
	out := make([]BCSection, 0)
	for rows.Next() {
		var s BCSection
		var termID, subjectName, courseCode, courseNumber, sectionNumber, sectionType sql.NullString
		var title, instructors, units, instructionMode sql.NullString
		var dates, days, mtime, loc, slug, desc sql.NullString
		if err := rows.Scan(&s.CCN, &termID, &subjectName, &courseCode, &courseNumber, &sectionNumber, &sectionType,
			&title, &instructors, &units, &instructionMode,
			&dates, &days, &mtime, &loc, &slug, &desc,
			&s.OpenSeats, &s.Enrolled, &s.Waitlisted, &s.Capacity, &s.LastSynced); err != nil {
			return nil, err
		}
		s.TermID = termID.String
		s.SubjectName = subjectName.String
		s.CourseCode = courseCode.String
		s.CourseNumber = courseNumber.String
		s.SectionNumber = sectionNumber.String
		s.SectionType = sectionType.String
		s.Title = title.String
		s.Instructors = instructors.String
		s.Units = units.String
		s.InstructionMode = instructionMode.String
		s.MeetingDates = dates.String
		s.MeetingDays = days.String
		s.MeetingTime = mtime.String
		s.Location = loc.String
		s.Slug = slug.String
		s.Description = desc.String
		out = append(out, s)
	}
	return out, rows.Err()
}

// QuerySectionsOpts is a small struct of filters; embed it directly.
type QuerySectionsOpts struct {
	CourseCode      string
	CourseCodeLike  string
	TermID          string
	InstructorLike  string
	SubjectNameLike string
	CCN             int
	OpenOnly        bool
	Limit           int
}

// LookupSectionByCCN returns one section by primary key, or nil row not found.
func LookupSectionByCCN(ctx context.Context, db *sql.DB, ccn int) (*BCSection, error) {
	rows, err := QuerySections(ctx, db, QuerySectionsOpts{CCN: ccn, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ListBCTerms returns every cached term row.
func ListBCTerms(ctx context.Context, db *sql.DB) ([]BCTermRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT term_id, name, COALESCE(kind,'') FROM bc_terms ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BCTermRow, 0)
	for rows.Next() {
		var r BCTermRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BCTermRow is one bc_terms row.
type BCTermRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

// ListBCSubjects returns every cached subject row.
func ListBCSubjects(ctx context.Context, db *sql.DB) ([]BCSubjectRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT subject_id, name, COALESCE(code,'') FROM bc_subjects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BCSubjectRow, 0)
	for rows.Next() {
		var r BCSubjectRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Code); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BCSubjectRow is one bc_subjects row.
type BCSubjectRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
}

// FindSubjectIDByName best-effort matches a subject name (case-insensitive
// substring) and returns its id, or "" if not found.
func FindSubjectIDByName(ctx context.Context, db *sql.DB, q string) (string, error) {
	if q == "" {
		return "", nil
	}
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT subject_id FROM bc_subjects WHERE LOWER(name) = LOWER(?) LIMIT 1`, q).Scan(&id)
	if err == sql.ErrNoRows {
		// Fall back to substring
		err = db.QueryRowContext(ctx,
			`SELECT subject_id FROM bc_subjects WHERE LOWER(name) LIKE LOWER(?) LIMIT 1`, "%"+q+"%").Scan(&id)
		if err == sql.ErrNoRows {
			return "", nil
		}
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// FindTermIDByName best-effort matches a term name and returns its id.
func FindTermIDByName(ctx context.Context, db *sql.DB, q string) (string, error) {
	if q == "" {
		return "", nil
	}
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT term_id FROM bc_terms WHERE LOWER(name) = LOWER(?) LIMIT 1`, q).Scan(&id)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx,
			`SELECT term_id FROM bc_terms WHERE LOWER(name) LIKE LOWER(?) LIMIT 1`, "%"+q+"%").Scan(&id)
		if err == sql.ErrNoRows {
			return "", nil
		}
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// LatestSnapshotsSince returns the most recent snapshot per CCN whose
// taken_at is at-or-after sinceUnix.
type SnapshotRow struct {
	CCN        int   `json:"ccn"`
	TakenAt    int64 `json:"taken_at"`
	OpenSeats  int   `json:"open_seats"`
	Enrolled   int   `json:"enrolled"`
	Waitlisted int   `json:"waitlisted"`
	Capacity   int   `json:"capacity"`
}

// SnapshotsSince returns every snapshot row with taken_at >= sinceUnix,
// ordered by ccn,taken_at ascending. Callers that only need the
// earliest-vs-latest pair per ccn should do that grouping in Go.
func SnapshotsSince(ctx context.Context, db *sql.DB, sinceUnix int64) ([]SnapshotRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT ccn, taken_at, open_seats, enrolled, waitlisted, capacity
		 FROM bc_section_snapshots
		 WHERE taken_at >= ?
		 ORDER BY ccn, taken_at`, sinceUnix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SnapshotRow, 0)
	for rows.Next() {
		var r SnapshotRow
		if err := rows.Scan(&r.CCN, &r.TakenAt, &r.OpenSeats, &r.Enrolled, &r.Waitlisted, &r.Capacity); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
