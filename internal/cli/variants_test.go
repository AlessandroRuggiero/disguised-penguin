package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeVariants(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dp"), 0755); err != nil {
		t.Fatalf("failed to create .dp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "variants.json"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write variants.json: %v", err)
	}
	return dir
}

func TestLoadVariantsMissingFile(t *testing.T) {
	got, err := loadVariants(t.TempDir())
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no variants, got %v", got)
	}
}

func TestLoadVariants(t *testing.T) {
	dir := writeVariants(t, `{
		"variants": [
			{
				"of": "claude",
				"build_file": "Dockerfile.dev",
				"local_mounts": {"./secrets": "/secrets"}
			},
			{
				"of": "opencode",
				"build_file": "Dockerfile.opencode"
			}
		]
	}`)

	got, err := loadVariants(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 variants, got %v", got)
	}
	v, ok := got["claude"]
	if !ok {
		t.Fatalf("expected a variant keyed by 'claude', got %v", got)
	}
	if v.BuildFile != "Dockerfile.dev" {
		t.Errorf("unexpected build file: %+v", v)
	}
	if v.LocalMounts["./secrets"] != "/secrets" {
		t.Errorf("unexpected local mounts: %v", v.LocalMounts)
	}
}

func TestLoadVariantsMalformed(t *testing.T) {
	if _, err := loadVariants(writeVariants(t, "{not json")); err == nil {
		t.Fatal("expected an error for malformed json")
	}
}

func TestLoadVariantsMissingOf(t *testing.T) {
	if _, err := loadVariants(writeVariants(t, `{"variants": [{"build_file": "Dockerfile"}]}`)); err == nil {
		t.Fatal("expected an error for a variant without 'of'")
	}
}

func TestImageRepository(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/you/claude:main":    "ghcr.io/you/claude",
		"ghcr.io/you/claude":         "ghcr.io/you/claude",
		"localhost:5000/claude":      "localhost:5000/claude",
		"localhost:5000/claude:main": "localhost:5000/claude",
		"claude@sha256:abc123":       "claude",
		"ghcr.io/You/Claude:Main":    "ghcr.io/you/claude",
	}
	for image, want := range cases {
		if got := imageRepository(image); got != want {
			t.Errorf("imageRepository(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestVariantImageReplacesTag(t *testing.T) {
	got := variantImage("ghcr.io/you/claude:main", t.TempDir())
	repo, tag, ok := strings.Cut(got, ":")
	if !ok || repo != "ghcr.io/you/claude" {
		t.Fatalf("expected the base tag to be replaced, got %q", got)
	}
	digest, hasPrefix := strings.CutPrefix(tag, variantTagPrefix)
	if !hasPrefix {
		t.Errorf("tag %q is missing the %q prefix", tag, variantTagPrefix)
	}
	if len(digest) != variantTagLen {
		t.Errorf("digest %q has length %d, want %d", digest, len(digest), variantTagLen)
	}
}

// The tag must not change with how the directory was spelled, or an unchanged
// project would look unbuilt and rebuild on every run.
func TestVariantImageStablePerDirectory(t *testing.T) {
	dir := t.TempDir()
	want := variantImage("claude", dir)
	for _, spelling := range []string{dir + "/", dir + "/.", filepath.Join(dir, "sub", "..")} {
		if got := variantImage("claude", spelling); got != want {
			t.Errorf("variantImage(_, %q) = %q, want %q", spelling, got, want)
		}
	}
	if variantImage("claude", t.TempDir()) == want {
		t.Error("expected different projects to get different tags")
	}
}

func TestLoadVariantsDuplicateOf(t *testing.T) {
	content := `{"variants": [{"of": "claude"}, {"of": "claude", "build_file": "Other"}]}`
	if _, err := loadVariants(writeVariants(t, content)); err == nil {
		t.Fatal("expected an error for two variants of the same CLI")
	}
}
