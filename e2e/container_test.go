//go:build integration

// Container integration suite: launches real containers through `dp` and asserts
// on their behavior, including mount-protection enforcement. Needs a working
// container runtime (docker/podman) and pulls a small image (busybox), so it is
// gated behind the `integration` build tag and, in CI, runs only on Linux.
//
// `dp` hardcodes `docker run -it`, so it needs a terminal. We give it one by
// launching it under a pseudo-terminal (creack/pty) — the only reason this file
// depends on that package, and the only reason it is confined to this tag.
//
// Run with:  go test -tags=integration -timeout 300s ./e2e/...
package e2e

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creack/pty"
)

const testImage = "busybox:latest"

func requireRuntime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err == nil {
		return
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return
	}
	t.Skip("no container runtime (docker/podman) available")
}

// addBusybox registers the busybox image as a CLI named "busy" in the isolated
// data dir. This is a plain DB write, so the non-PTY helper is fine.
func addBusybox(t *testing.T, data string) {
	t.Helper()
	out, code := run(t, data, "add", "busy", testImage)
	mustOK(t, "Successfully added CLI 'busy'", out, code)
}

// runPTY launches `dp` under a pseudo-terminal with cwd set to dir (which becomes
// the /workspace bind mount) and an isolated XDG_DATA_HOME. It returns the merged
// terminal output and the process exit status.
func runPTY(t *testing.T, dir, dataHome string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(dpBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dataHome)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", err
	}
	defer ptmx.Close()

	// Nothing writes to stdin and the test commands don't read it, so nothing
	// blocks. Reading the master until the child exits ends in an EIO on Linux;
	// that's expected, so the copy error is ignored and cmd.Wait() is the source
	// of truth for the exit status.
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, ptmx)
	return buf.String(), cmd.Wait()
}

func TestContainer_RunsAndExitsZero(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	addBusybox(t, data)

	// dp busy echo hello-from-container
	out, err := runPTY(t, dir, data, "busy", "echo", "hello-from-container")
	if err != nil {
		t.Fatalf("dp busy echo failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "hello-from-container") {
		t.Fatalf("expected container stdout in output; got:\n%s", out)
	}
}

func TestContainer_WorkspaceMountIsReadable(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("MOUNT_OK"), 0o644); err != nil {
		t.Fatal(err)
	}
	addBusybox(t, data)

	// dp busy cat /workspace/marker.txt
	out, err := runPTY(t, dir, data, "busy", "cat", "/workspace/marker.txt")
	if err != nil {
		t.Fatalf("cat failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "MOUNT_OK") {
		t.Fatalf("workspace file not readable in container; got:\n%s", out)
	}
}

func TestContainer_MountProtectionReadOnly(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "locked"), 0o755); err != nil {
		t.Fatal(err)
	}
	addBusybox(t, data)

	// A write to an unprotected path succeeds.
	// dp busy sh -c 'touch /workspace/free && echo WRITE_OK'
	out, err := runPTY(t, dir, data, "busy", "sh", "-c", "touch /workspace/free && echo WRITE_OK")
	if err != nil {
		t.Fatalf("unprotected write failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "WRITE_OK") {
		t.Fatalf("expected unprotected write to succeed; got:\n%s", out)
	}

	// The same write under a :ro protection is blocked by the runtime.
	// dp --mp locked:ro busy sh -c 'touch /workspace/locked/x 2>&1 || echo WRITE_BLOCKED'
	out, err = runPTY(t, dir, data, "--mp", "locked:ro", "busy", "sh", "-c", "touch /workspace/locked/x 2>&1 || echo WRITE_BLOCKED")
	if err != nil {
		t.Fatalf("protected run errored unexpectedly: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "WRITE_BLOCKED") {
		t.Fatalf("expected read-only mount to block the write; got:\n%s", out)
	}
}

func TestContainer_MountProtectionHide(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret", "data.txt"), []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	addBusybox(t, data)

	// Without hiding, the secret is visible.
	// dp busy cat /workspace/secret/data.txt
	out, err := runPTY(t, dir, data, "busy", "cat", "/workspace/secret/data.txt")
	if err != nil {
		t.Fatalf("baseline read failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "TOPSECRET") {
		t.Fatalf("expected secret visible without protection; got:\n%s", out)
	}

	// With :h the path is shadowed by an empty mount, so the file is gone.
	// dp --mp secret:h busy sh -c 'cat /workspace/secret/data.txt 2>&1 || echo HIDDEN'
	out, err = runPTY(t, dir, data, "--mp", "secret:h", "busy", "sh", "-c", "cat /workspace/secret/data.txt 2>&1 || echo HIDDEN")
	if err != nil {
		t.Fatalf("hide run errored unexpectedly: %v\noutput:\n%s", err, out)
	}
	if strings.Contains(out, "TOPSECRET") {
		t.Fatalf("secret should be hidden but was readable; got:\n%s", out)
	}
	if !strings.Contains(out, "HIDDEN") {
		t.Fatalf("expected hidden path to make the file absent; got:\n%s", out)
	}
}
