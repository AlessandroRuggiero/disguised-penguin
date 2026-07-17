//go:build smoke || integration

// Shared scaffolding for both the smoke suite (-tags=smoke) and the container
// integration suite (-tags=integration): it builds the real `dp` binary once
// and provides helpers to invoke it against an isolated XDG_DATA_HOME.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Seeded by migrations/001_init.sql on first run.
const defaultRegistryURI = "https://raw.githubusercontent.com/AlessandroRuggiero/disguised-penguin-repo/main"

var dpBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "dp-e2e-build")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	dpBin = filepath.Join(tmp, "dp")
	if runtime.GOOS == "windows" {
		dpBin += ".exe"
	}
	build := exec.Command("go", "build", "-o", dpBin, "disguised-penguin/cmd/disguised-penguin")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("failed to build dp for e2e test: " + err.Error())
	}

	os.Exit(m.Run())
}

func run(t *testing.T, dataHome string, args ...string) (string, int) {
	t.Helper()
	return runInput(t, dataHome, "", args...)
}

func runInput(t *testing.T, dataHome, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(dpBin, args...)
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dataHome)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("dp %v: unexpected exec error: %v", args, err)
	}
	return string(out), code
}

func mustOK(t *testing.T, want, out string, code int) {
	t.Helper()
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, out)
	}
	if want != "" && !strings.Contains(out, want) {
		t.Fatalf("output missing %q; got:\n%s", want, out)
	}
}

func mustFail(t *testing.T, want, out string, code int) {
	t.Helper()
	if code == 0 {
		t.Fatalf("expected non-zero exit; output:\n%s", out)
	}
	if want != "" && !strings.Contains(out, want) {
		t.Fatalf("output missing %q; got:\n%s", want, out)
	}
}
