package cmd

import (
	"strings"
	"testing"
)

// ── formatTokenCount ──────────────────────────────────────────────────────────

func TestFormatTokenCount(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1_000, "1.0K"},
		{1_500, "1.5K"},
		{31_000, "31.0K"},
		{999_999, "1000.0K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, c := range cases {
		got := formatTokenCount(c.n)
		if got != c.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// ── parseReports ─────────────────────────────────────────────────────────────

func TestParseReports_all(t *testing.T) {
	got := parseReports("all")
	if got == nil {
		t.Fatal("expected non-nil map")
	}
	for _, key := range []string{"onboarding", "improvement", "claude"} {
		if !got[key] {
			t.Errorf("expected %q in 'all' result", key)
		}
	}
	if got["all"] {
		t.Error("'all' key should not appear in expanded result")
	}
}

func TestParseReports_specific(t *testing.T) {
	got := parseReports("onboarding,claude")
	if got == nil {
		t.Fatal("expected non-nil map")
	}
	if !got["onboarding"] {
		t.Error("expected 'onboarding' in result")
	}
	if !got["claude"] {
		t.Error("expected 'claude' in result")
	}
	if got["improvement"] {
		t.Error("'improvement' should not be in result")
	}
}

func TestParseReports_invalid(t *testing.T) {
	got := parseReports("invalid")
	if got != nil {
		t.Errorf("expected nil for invalid report name, got %v", got)
	}
}

func TestParseReports_mixedCase(t *testing.T) {
	// Parsing is case-insensitive.
	got := parseReports("Onboarding,CLAUDE")
	if got == nil {
		t.Fatal("expected non-nil map for case-insensitive input")
	}
	if !got["onboarding"] {
		t.Error("expected 'onboarding' (lowercased) in result")
	}
	if !got["claude"] {
		t.Error("expected 'claude' (lowercased) in result")
	}
}

func TestParseReports_whitespace(t *testing.T) {
	got := parseReports("onboarding, claude")
	if got == nil {
		t.Fatal("expected non-nil map with whitespace around entries")
	}
	if !got["onboarding"] || !got["claude"] {
		t.Error("whitespace around entries should be trimmed")
	}
}

func TestParseReports_invalidMixed(t *testing.T) {
	// One invalid entry makes the whole parse fail.
	got := parseReports("onboarding,bogus")
	if got != nil {
		t.Errorf("expected nil when any entry is invalid, got %v", got)
	}
}

// ── termLink ──────────────────────────────────────────────────────────────────

func TestTermLink(t *testing.T) {
	url := "file:///tmp/ANALYSIS.md"
	text := "ANALYSIS.md"
	got := termLink(url, text)

	if !strings.Contains(got, url) {
		t.Errorf("termLink output missing URL %q; got %q", url, got)
	}
	if !strings.Contains(got, text) {
		t.Errorf("termLink output missing display text %q; got %q", text, got)
	}
	// OSC 8 sequences use \033]8 prefix.
	if !strings.Contains(got, "\033]8") {
		t.Error("termLink output missing OSC 8 escape sequence")
	}
}
