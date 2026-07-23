package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"disguised-penguin/internal/container"
)

func writeMounts(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dp"), 0755); err != nil {
		t.Fatalf("failed to create .dp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "mounts.json"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write mounts.json: %v", err)
	}
	return dir
}

func TestLoadLocalMountsMissingFile(t *testing.T) {
	got, err := loadLocalMounts(t.TempDir())
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no mounts, got %v", got)
	}
}

func TestLoadLocalMounts(t *testing.T) {
	dir := writeMounts(t, `{
		"mounts": [
			{"source": "./secrets", "path": "config/secrets", "mode": "ro"},
			{"source": "cache", "path": "cache", "mode": "rw"},
			{"path": "node_modules", "mode": "h"}
		]
	}`)

	got, err := loadLocalMounts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sorted by destination.
	want := []localMount{
		{Source: "cache", Dest: "cache", Mode: "rw"},
		{Source: "secrets", Dest: "config/secrets", Mode: "ro"},
		{Source: "", Dest: "node_modules", Mode: "h"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mount %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadLocalMountsInvalid(t *testing.T) {
	cases := map[string]string{
		"malformed json":   `{not json`,
		"missing path":     `{"mounts": [{"source": "secrets", "mode": "ro"}]}`,
		"bad mode":         `{"mounts": [{"path": "s", "mode": "x"}]}`,
		"hide with source": `{"mounts": [{"source": "secrets", "path": "s", "mode": "h"}]}`,
		"absolute source":  `{"mounts": [{"source": "/etc", "path": "s", "mode": "ro"}]}`,
		"escaping source":  `{"mounts": [{"source": "../secrets", "path": "s", "mode": "ro"}]}`,
		"escaping dest":    `{"mounts": [{"source": "secrets", "path": "../out", "mode": "ro"}]}`,
		"root dest":        `{"mounts": [{"path": ".", "mode": "h"}]}`,
	}
	for name, content := range cases {
		if _, err := loadLocalMounts(writeMounts(t, content)); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

func TestBuildWorkspaceOverlaysLocalHide(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "node_modules"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))

	mounts := []localMount{{Dest: "node_modules", Mode: "h"}}
	args, err := buildWorkspaceOverlays(cwd, false, hides, nil, mounts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hideArg, ok := mountArgFor(args, "/workspace/node_modules")
	if !ok {
		t.Fatalf("no overlay at /workspace/node_modules in %v", args)
	}
	// Hidden by an empty placeholder, not bound through to the real folder.
	if strings.HasPrefix(hideArg, filepath.Join(cwd, "node_modules")+":") {
		t.Errorf("node_modules should be hidden, not bound through: %q", hideArg)
	}
}

func TestBuildWorkspaceOverlaysDpReadOnly(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".dp"), 0755); err != nil {
		t.Fatalf("failed to create .dp: %v", err)
	}
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))

	// With no protections or mounts at all, .dp is still bound read-only.
	args, err := buildWorkspaceOverlays(cwd, false, hides, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mountArgFor(args, "/workspace/.dp")
	if !ok {
		t.Fatalf("expected a default overlay at /workspace/.dp in %v", args)
	}
	if !strings.HasPrefix(got, filepath.Join(cwd, ".dp")+":") || !strings.HasSuffix(got, ":ro") {
		t.Errorf("expected .dp bound read-only, got %q", got)
	}
}

func TestBuildWorkspaceOverlaysDpAbsentSkipped(t *testing.T) {
	cwd := t.TempDir() // no .dp/ here
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))
	args, err := buildWorkspaceOverlays(cwd, false, hides, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 0 {
		t.Errorf("expected no overlays when .dp is absent, got %v", args)
	}
}

// The read-only .dp default is the lowest priority, so a --mp can re-open it.
func TestBuildWorkspaceOverlaysDpOverridable(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".dp"), 0755); err != nil {
		t.Fatalf("failed to create .dp: %v", err)
	}
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))

	args, err := buildWorkspaceOverlays(cwd, false, hides, nil, nil, []string{".dp:rw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mountArgFor(args, "/workspace/.dp")
	if !ok {
		t.Fatalf("no overlay at /workspace/.dp in %v", args)
	}
	if !strings.HasSuffix(got, ":rw") {
		t.Errorf("expected --mp to re-open .dp read-write, got %q", got)
	}
	if n := strings.Count(strings.Join(args, "\n"), "/workspace/.dp:"); n != 1 {
		t.Errorf("expected one overlay at /workspace/.dp, got %d in %v", n, args)
	}
}

// mountArgFor returns the overlay -v value bound at the given container dest.
func mountArgFor(args []string, dest string) (string, bool) {
	for _, a := range args {
		if strings.Contains(a, ":"+dest+":") || strings.HasSuffix(a, ":"+dest) {
			return a, true
		}
	}
	return "", false
}

func TestBuildWorkspaceOverlaysLocalMount(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".dp", "secrets"), 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))

	mounts := []localMount{{Source: "secrets", Dest: "config", Mode: "ro"}}
	args, err := buildWorkspaceOverlays(cwd, false, hides, nil, mounts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The real content is bound read-only at the destination.
	mountArg, ok := mountArgFor(args, "/workspace/config")
	if !ok {
		t.Fatalf("no mount at /workspace/config in %v", args)
	}
	if !strings.HasPrefix(mountArg, filepath.Join(cwd, ".dp", "secrets")+":") || !strings.HasSuffix(mountArg, ":ro") {
		t.Errorf("unexpected mount arg %q", mountArg)
	}
	// The source under .dp is hidden by an empty placeholder.
	hideArg, ok := mountArgFor(args, "/workspace/.dp/secrets")
	if !ok {
		t.Fatalf("no hide overlay at /workspace/.dp/secrets in %v", args)
	}
	if strings.HasPrefix(hideArg, filepath.Join(cwd, ".dp", "secrets")+":") {
		t.Errorf("source should be hidden, not bound through: %q", hideArg)
	}
}

func TestBuildWorkspaceOverlaysMissingSource(t *testing.T) {
	cwd := t.TempDir()
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))
	mounts := []localMount{{Source: "secrets", Dest: "config", Mode: "ro"}}
	if _, err := buildWorkspaceOverlays(cwd, false, hides, nil, mounts, nil); err == nil {
		t.Fatal("expected an error for a missing .dp source folder")
	}
}

// Args-level protections must win over .dp mounts, which win over workspace-level
// protections, on a shared destination.
func TestBuildWorkspaceOverlaysPriority(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "shared"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".dp", "src"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	hides := container.NewHidePlaceholders(filepath.Join(t.TempDir(), "placeholders"))
	mounts := []localMount{{Source: "src", Dest: "shared", Mode: "ro"}}

	// Workspace hides /workspace/shared, .dp mounts over it rw, args force it ro.
	args, err := buildWorkspaceOverlays(cwd, false, hides, []string{"shared:h"}, mounts, []string{"shared:ro"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mountArgFor(args, "/workspace/shared")
	if !ok {
		t.Fatalf("no overlay at /workspace/shared in %v", args)
	}
	// Exactly one overlay per destination (the runtime rejects duplicates).
	if n := strings.Count(strings.Join(args, "\n"), "/workspace/shared:"); n != 1 {
		t.Errorf("expected one overlay at /workspace/shared, got %d in %v", n, args)
	}
	// The args-level ro protection wins: it binds the real host path read-only.
	if !strings.HasPrefix(got, filepath.Join(cwd, "shared")+":") || !strings.HasSuffix(got, ":ro") {
		t.Errorf("expected args-level ro to win, got %q", got)
	}
}
