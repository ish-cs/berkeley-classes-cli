// Copyright 2026 ish-cs. MIT License. See LICENSE.

package types

type FacetSnapshot struct {
	Url   string `json:"url"`
	Title string `json:"title"`
}

type SearchPage struct {
	Url         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type SectionDetail struct {
	Url         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
