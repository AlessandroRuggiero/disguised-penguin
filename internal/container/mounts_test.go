package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProtection_Valid(t *testing.T) {
	cases := []struct {
		spec     string
		wantRel  string
		wantMode string
	}{
		{".git:ro", ".git", "ro"},
		{".env:h", ".env", "h"},
		{"src/secret.txt:rw", "src/secret.txt", "rw"},
		{"./config:ro", "config", "ro"},
		{"a/b/../c:ro", "a/c", "ro"},
	}
	for _, tc := range cases {
		got, err := ParseProtection(tc.spec)
		if err != nil {
			t.Fatalf("ParseProtection(%q) unexpected error: %v", tc.spec, err)
		}
		if got.Rel != tc.wantRel || got.Mode != tc.wantMode {
			t.Errorf("ParseProtection(%q) = {%q, %q}, want {%q, %q}", tc.spec, got.Rel, got.Mode, tc.wantRel, tc.wantMode)
		}
	}
}

func TestParseProtection_Invalid(t *testing.T) {
	cases := []string{
		".git",             // no mode
		".git:",            // empty mode
		":ro",              // empty path
		".git:xx",          // bad mode
		"/etc/passwd:ro",   // absolute
		"../secrets:ro",    // escapes workspace
		"../../x:h",        // escapes workspace
		".:ro",             // workspace root
	}
	for _, spec := range cases {
		if _, err := ParseProtection(spec); err == nil {
			t.Errorf("ParseProtection(%q) expected error, got nil", spec)
		}
	}
}

func TestMountOpts(t *testing.T) {
	cases := []struct {
		mode    string
		selinux bool
		want    string
	}{
		{"", false, ""},
		{"", true, ":z"},
		{"ro", false, ":ro"},
		{"ro", true, ":ro,z"},
		{"rw", true, ":rw,z"},
	}
	for _, tc := range cases {
		if got := MountOpts(tc.mode, tc.selinux); got != tc.want {
			t.Errorf("MountOpts(%q, %v) = %q, want %q", tc.mode, tc.selinux, got, tc.want)
		}
	}
}

func TestHidePlaceholders(t *testing.T) {
	base := filepath.Join(t.TempDir(), "placeholders")
	h := NewHidePlaceholders(base)

	dir, err := h.Get(true)
	if err != nil {
		t.Fatalf("Get(dir) error: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected empty dir placeholder at %q: info=%v err=%v", dir, info, err)
	}
	if !strings.HasPrefix(dir, base) {
		t.Errorf("dir placeholder %q not under base %q", dir, base)
	}

	file, err := h.Get(false)
	if err != nil {
		t.Fatalf("Get(file) error: %v", err)
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() || info.Size() != 0 {
		t.Errorf("expected empty file placeholder at %q: info=%v err=%v", file, info, err)
	}
	if !strings.HasPrefix(file, base) {
		t.Errorf("file placeholder %q not under base %q", file, base)
	}

	// Get is idempotent: a second call on the reused location must not error.
	if _, err := h.Get(true); err != nil {
		t.Errorf("second Get(dir) error: %v", err)
	}
	if _, err := h.Get(false); err != nil {
		t.Errorf("second Get(file) error: %v", err)
	}
}
