package cli

import (
	"path/filepath"
	"testing"
	"time"

	"disguised-penguin/internal/models"
)

func TestParseAge(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{"30d": 30 * day, "2w": 14 * day, "0d": 0, "36h": 36 * time.Hour, "90m": 90 * time.Minute} {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "-3d", "abc", "3x", "1.5d", "-1h"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q): expected error", in)
		}
	}
}

func TestPrunable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	day := 24 * time.Hour
	variants := []models.TrackedVariant{
		{ID: 1, ProjectDir: "/recent-run", BuiltAt: now.Add(-100 * day), LastUsedAt: now.Add(-1 * day)},
		{ID: 2, ProjectDir: "/old-run", BuiltAt: now.Add(-100 * day), LastUsedAt: now.Add(-40 * day)},
		{ID: 3, ProjectDir: "/never-run-old", BuiltAt: now.Add(-40 * day)},
		{ID: 4, ProjectDir: "/never-run-new", BuiltAt: now.Add(-1 * day)},
		{ID: 5, ProjectDir: "/gone", BuiltAt: now, LastUsedAt: now},
	}
	exists := func(dir string) bool { return dir != "/gone" }

	var ids []int
	for _, v := range prunable(variants, now, 30*day, exists) {
		ids = append(ids, v.ID)
	}
	if want := []int{2, 3, 5}; len(ids) != len(want) || ids[0] != 2 || ids[1] != 3 || ids[2] != 5 {
		t.Errorf("got %v, want %v", ids, want)
	}
}

func TestProjectKeyMatchesVariantImage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	key := projectKey(".")
	if key != filepath.Clean(dir) {
		t.Errorf("projectKey(.) = %q, want %q", key, dir)
	}
	// The table stores projectKey and the tag hashes it, so "." and the full
	// path must give the same image.
	if variantImage("img", ".") != variantImage("img", key) {
		t.Errorf("variantImage differs between . and %s", key)
	}
}

func TestFormatAgo(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := map[string]time.Time{
		"never":    {},
		"just now": now.Add(-10 * time.Second),
		"5m ago":   now.Add(-5 * time.Minute),
		"3h ago":   now.Add(-3 * time.Hour),
		"12d ago":  now.Add(-12 * 24 * time.Hour),
	}
	for want, at := range cases {
		if got := formatAgo(at, now); got != want {
			t.Errorf("formatAgo(%v) = %q, want %q", at, got, want)
		}
	}
}
