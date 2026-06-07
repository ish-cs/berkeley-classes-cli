// Copyright 2026 ish-cs. MIT License. See LICENSE.

package bsource

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	digitsRE     = regexp.MustCompile(`\d+`)
	leadDigitsRE = regexp.MustCompile(`^\d+`)
	classNumRE   = regexp.MustCompile(`#(\d+)`)
	whitespaceRE = regexp.MustCompile(`\s+`)
)

// cleanText collapses runs of whitespace and trims; html entities are decoded.
func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func firstInt(s string) int {
	m := leadDigitsRE.FindString(strings.TrimSpace(s))
	if m == "" {
		// Fall back to any digit run
		m = digitsRE.FindString(s)
		if m == "" {
			return 0
		}
	}
	n, _ := strconv.Atoi(m)
	return n
}

// ParseSearch parses one /search/class HTML page into SearchResults.
func ParseSearch(r io.Reader) (*SearchResults, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}
	res := &SearchResults{Cards: make([]SectionCard, 0)}

	doc.Find("article.st").Each(func(_ int, s *goquery.Selection) {
		card := parseCard(s)
		if card.CCN == 0 && card.CourseCode == "" {
			return
		}
		res.Cards = append(res.Cards, card)
	})

	// Pagination: next link in pager
	if next := doc.Find("li.pager__item--next a, a[rel=next]").First(); next.Length() > 0 {
		res.HasNextPage = true
	}

	// Total label (best-effort): "5,784 results"
	if t := doc.Find(".view-header, .views-summary").First(); t.Length() > 0 {
		res.TotalLabel = cleanText(t.Text())
	}
	return res, nil
}

// parseCard turns one <article.st> selection into a SectionCard.
func parseCard(s *goquery.Selection) SectionCard {
	c := SectionCard{}
	c.NodeID, _ = s.Attr("data-history-node-id")

	c.TermName = cleanText(s.Find(".st--term-year").First().Text())

	// CCN comes from either ".st--section-number" near the top
	// (which begins with #) or the inner "Class #:" span. Prefer the latter
	// because the top one occasionally carries a "Units:" span instead.
	s.Find(".st--section-info-wrapper .st--section-number").EachWithBreak(func(_ int, n *goquery.Selection) bool {
		txt := cleanText(n.Text())
		txt = strings.TrimPrefix(txt, "Class #:")
		if v := firstInt(txt); v > 0 {
			c.CCN = v
			return false
		}
		return true
	})
	if c.CCN == 0 {
		// Fall back to the top "#29147" header
		top := cleanText(s.Find(".st--term .st--section-number").First().Text())
		if m := classNumRE.FindStringSubmatch(top); len(m) > 1 {
			c.CCN, _ = strconv.Atoi(m[1])
		}
	}

	// Section-name anchor: href + nested spans
	anchor := s.Find(".st--section-name-wraper a").First()
	if anchor.Length() > 0 {
		href, _ := anchor.Attr("href")
		c.Slug = strings.TrimPrefix(href, "/content/")
		c.CourseCode = cleanText(anchor.Find(".st--section-name").First().Text())
		counts := anchor.Find(".st--section-count")
		if counts.Length() >= 1 {
			c.SectionNumber = cleanText(counts.Eq(0).Text())
		}
		if counts.Length() >= 2 {
			c.SectionSubNum = cleanText(counts.Eq(1).Text())
		}
		c.SectionType = cleanText(anchor.Find(".st--section-code").First().Text())
	}

	// Department: ".st-section-dept a" (sometimes absent)
	if d := s.Find(".st-section-dept a").First(); d.Length() > 0 {
		c.Department = cleanText(d.Text())
		c.DepartmentURL, _ = d.Attr("href")
	}

	c.Title = cleanText(s.Find(".st--title h2").First().Text())

	if instr := s.Find(".st--instructors").First(); instr.Length() > 0 {
		// Strip the icon span; .Text() already discards tag names
		c.Instructors = cleanText(instr.Text())
	}

	c.MeetingDates = cleanText(s.Find(".st--meeting-dates").First().Text())
	c.MeetingDays = cleanText(s.Find(".st--meeting-days span").Not(".icon").First().Text())
	c.MeetingTime = cleanText(s.Find(".st--meeting-time span").Not(".icon").First().Text())

	if loc := s.Find(".st--location").First(); loc.Length() > 0 {
		if a := loc.Find("a").First(); a.Length() > 0 {
			c.Location = cleanText(a.Text())
			c.LocationURL, _ = a.Attr("href")
		} else {
			// Sometimes location is plain text (no map link)
			text := cleanText(loc.Text())
			c.Location = text
		}
	}

	// Units lives inside ".st--details-unit" span (alongside the Units: label)
	s.Find(".st--details-unit").EachWithBreak(func(_ int, n *goquery.Selection) bool {
		txt := cleanText(n.Text())
		txt = strings.TrimPrefix(txt, "Units:")
		txt = strings.TrimSpace(txt)
		if txt != "" {
			c.Units = txt
			return false
		}
		return true
	})

	// Extras: instruction mode + time-conflict flag
	s.Find(".st--extras p").Each(func(_ int, p *goquery.Selection) {
		strong := cleanText(p.Find("strong").First().Text())
		if strings.HasPrefix(strings.ToLower(strong), "instruction mode") {
			c.InstructionMode = cleanText(p.Find("span").First().Text())
		}
		if strings.Contains(strings.ToLower(strong), "time conflict enrollment allowed") {
			c.TimeConflictAllowed = true
		}
	})

	// Seats: two <p>s — first is label, second is value "65 Unreserved Seats"
	seats := s.Find(".st--seats p")
	if seats.Length() >= 1 {
		c.OpenSeatsLabel = cleanText(seats.Eq(0).Text())
	}
	if seats.Length() >= 2 {
		v := cleanText(seats.Eq(1).Text())
		c.OpenSeats = firstInt(v)
	}

	c.Description = cleanText(s.Find(".st--description").First().Text())
	return c
}

// ParseDetail parses a single section detail page (/content/<slug>).
func ParseDetail(r io.Reader) (*SectionDetail, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}
	d := &SectionDetail{}

	// The detail page reuses much of the same selectors but uses
	// the "sf" prefix instead of "st" for the article. We accept either.
	article := doc.Find("article.sf, article.st").First()
	if article.Length() > 0 {
		// Try the .st--* selectors first; if missing, parseCard returns zeros
		// and we backfill from the .sf--* equivalents.
		// Just reuse parseCard against the article — many CSS class names
		// overlap or are identical on the detail page in practice.
		d.Card = parseCard(article)
	}

	// Current-enrollment block
	doc.Find("section.collapsable.current-enrollment .stats div").Each(func(_ int, n *goquery.Selection) {
		strong := cleanText(n.Find("strong").First().Text())
		num := firstInt(cleanText(n.Text()))
		k := strings.ToLower(strong)
		switch {
		case strings.Contains(k, "enrolled"):
			d.Enrolled = num
		case strings.Contains(k, "waitlist max"):
			d.WaitlistMax = num
		case strings.Contains(k, "waitlist"):
			d.Waitlisted = num
		case strings.Contains(k, "capacity"):
			d.Capacity = num
		}
	})
	if top := doc.Find("section.collapsable.current-enrollment .top span").First(); top.Length() > 0 {
		if v := firstInt(cleanText(top.Text())); v > 0 {
			d.Card.OpenSeats = v
		}
	}

	// Reserved-seat breakdown
	parts := make([]string, 0)
	doc.Find(".details > .detail-numeral").Each(func(_ int, n *goquery.Selection) {
		t := cleanText(n.Text())
		if t != "" {
			parts = append(parts, t)
		}
	})
	if len(parts) > 0 {
		d.ReserveDetail = strings.Join(parts, "; ")
	}
	return d, nil
}

// ParseFacets extracts every facet sidebar list from a search-page HTML.
func ParseFacets(r io.Reader) (*FacetSnapshot, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}
	snap := &FacetSnapshot{}
	doc.Find("[data-drupal-facet-alias]").Each(func(_ int, ul *goquery.Selection) {
		alias, _ := ul.Attr("data-drupal-facet-alias")
		items := make([]FacetItem, 0)
		// Items live either as <button data-drupal-facet-item-value=...>
		// (when active) or as <a data-drupal-facet-item-value=...> in nested ul.
		ul.Find("[data-drupal-facet-item-value]").Each(func(_ int, n *goquery.Selection) {
			val, _ := n.Attr("data-drupal-facet-item-value")
			countAttr, _ := n.Attr("data-drupal-facet-item-count")
			name := cleanText(n.Find(".facet-item__value").First().Text())
			if name == "" {
				name = cleanText(n.Text())
			}
			cnt, _ := strconv.Atoi(countAttr)
			if val == "" || name == "" {
				return
			}
			items = append(items, FacetItem{ID: val, Name: name, Count: cnt})
		})
		dedup := dedupFacets(items)
		switch alias {
		case "term":
			snap.Terms = append(snap.Terms, dedup...)
		case "subject_area":
			snap.SubjectAreas = append(snap.SubjectAreas, dedup...)
		case "course_level":
			snap.CourseLevel = append(snap.CourseLevel, dedup...)
		case "course_types":
			snap.CourseTypes = append(snap.CourseTypes, dedup...)
		case "meets_days":
			snap.MeetsDays = append(snap.MeetsDays, dedup...)
		case "units":
			snap.Units = append(snap.Units, dedup...)
		case "enrollment_status":
			snap.EnrollmentStatus = append(snap.EnrollmentStatus, dedup...)
		case "mode_instruction":
			snap.ModeInstruction = append(snap.ModeInstruction, dedup...)
		case "breadth_requirements":
			snap.BreadthRequirements = append(snap.BreadthRequirements, dedup...)
		case "course_threads":
			snap.CourseThreads = append(snap.CourseThreads, dedup...)
		}
	})

	// Dedup the global slices (the sidebar may repeat list elements).
	snap.Terms = dedupFacets(snap.Terms)
	snap.SubjectAreas = dedupFacets(snap.SubjectAreas)
	snap.CourseLevel = dedupFacets(snap.CourseLevel)
	snap.CourseTypes = dedupFacets(snap.CourseTypes)
	snap.MeetsDays = dedupFacets(snap.MeetsDays)
	snap.Units = dedupFacets(snap.Units)
	snap.EnrollmentStatus = dedupFacets(snap.EnrollmentStatus)
	snap.ModeInstruction = dedupFacets(snap.ModeInstruction)
	snap.BreadthRequirements = dedupFacets(snap.BreadthRequirements)
	snap.CourseThreads = dedupFacets(snap.CourseThreads)
	return snap, nil
}

func dedupFacets(items []FacetItem) []FacetItem {
	seen := make(map[string]struct{}, len(items))
	out := make([]FacetItem, 0, len(items))
	for _, it := range items {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	return out
}
