// Copyright 2026 ish-cs. MIT License. See LICENSE.

package bsource

import "testing"

func TestCleanText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  hello   world  ", "hello world"},
		{"a\tb\nc", "a b c"},
		{"&amp;", "&"},
		{"", ""},
	}
	for _, c := range cases {
		got := cleanText(c.in)
		if got != c.want {
			t.Errorf("cleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFirstInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"123abc", 123},
		{"x456", 456},
		{"no digits", 0},
		{"  789  ", 789},
		{"0", 0},
	}
	for _, c := range cases {
		got := firstInt(c.in)
		if got != c.want {
			t.Errorf("firstInt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
