// Copyright 2026 ish-cs. MIT License. See LICENSE.

package cli

import (
	"strconv"
	"strings"
)

// canonicalDays normalizes Berkeley-style day codes (e.g. "Mo, We, Fr",
// "Tu/Th", "Mon Wed Fri") to the canonical two-letter form: Mo, Tu, We,
// Th, Fr, Sa, Su.
func parseMeetingDays(s string) []string {
	if s == "" {
		return nil
	}
	// Normalize separators
	for _, sep := range []string{",", "/", "|", "·"} {
		s = strings.ReplaceAll(s, sep, " ")
	}
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		short := canonicalDayCode(p)
		if short == "" || seen[short] {
			continue
		}
		seen[short] = true
		out = append(out, short)
	}
	return out
}

func canonicalDayCode(p string) string {
	// Most Berkeley pages already use "Mo Tu We Th Fr Sa Su" form.
	lower := strings.ToLower(p)
	switch {
	case strings.HasPrefix(lower, "mo"):
		return "Mo"
	case strings.HasPrefix(lower, "tu"):
		return "Tu"
	case strings.HasPrefix(lower, "we"):
		return "We"
	case strings.HasPrefix(lower, "th"):
		return "Th"
	case strings.HasPrefix(lower, "fr"):
		return "Fr"
	case strings.HasPrefix(lower, "sa"):
		return "Sa"
	case strings.HasPrefix(lower, "su"):
		return "Su"
	}
	return ""
}

// parseMeetingTime parses "12:00 pm - 12:59 pm" (or 24-hour) into
// minutes-since-midnight pairs. Returns ok=false if the time is missing,
// empty, or unparseable.
func parseMeetingTime(s string) (start, end int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	// Normalize en-dash + multiple spaces
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.Join(strings.Fields(s), " ")
	// Split on " - "
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	startMin, ok1 := parseClock(parts[0])
	endMin, ok2 := parseClock(parts[1])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return startMin, endMin, true
}

// parseClock handles "12:00 pm", "1:30 PM", "13:00".
func parseClock(s string) (int, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false
	}
	suffix := ""
	if strings.HasSuffix(s, "am") || strings.HasSuffix(s, "pm") {
		suffix = s[len(s)-2:]
		s = strings.TrimSpace(s[:len(s)-2])
	}
	hh, mm := 0, 0
	if i := strings.Index(s, ":"); i >= 0 {
		h, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, false
		}
		m, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if err != nil {
			return 0, false
		}
		hh, mm = h, m
	} else {
		h, err := strconv.Atoi(s)
		if err != nil {
			return 0, false
		}
		hh = h
	}
	switch suffix {
	case "pm":
		if hh < 12 {
			hh += 12
		}
	case "am":
		if hh == 12 {
			hh = 0
		}
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// intersectDays returns the canonical-coded days present in both a and b.
func intersectDays(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]bool, len(a))
	for _, d := range a {
		set[d] = true
	}
	out := make([]string, 0)
	for _, d := range b {
		if set[d] {
			out = append(out, d)
		}
	}
	return out
}
