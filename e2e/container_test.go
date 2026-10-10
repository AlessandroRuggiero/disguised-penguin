//go:build integration

// Container integration suite: launches real containers through `dp` and asserts
// on their behavior, including mount-protection enforcement. Needs a working
// container runtime (docker/podman) and pulls a small image (busybox), so it is
// gated behind the `integration` build tag and, in CI, runs only on Linux.
//
// `dp` only passes `-t` to the runtime when it is attached to a terminal. We
// give it one by launching it under a pseudo-terminal (creack/pty), so these
// tests exercise the interactive path users normally hit — the only reason this
// file depends on that package, and the only reason it is confined to this tag.
//
// Run with:  go test -tags=integration -timeout 300s ./e2e/...
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// runtimeName mirrors dp's auto-detection: docker if present, else podman.
func runtimeName() string {
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	return "podman"
}

// variantRef recomputes the tag dp gives a variant of busybox built in dir.
func variantRef(dir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return "busybox:variant-" + hex.EncodeToString(sum[:])[:32]
}

func TestContainer_VariantDestroy(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	addBusybox(t, data)

	if err := os.MkdirAll(filepath.Join(dir, ".dp", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "variants.json"), []byte(`{"variants": {"busy": {"build_file": ".dp/build/busy.Dockerfile"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "build", "busy.Dockerfile"), []byte("FROM "+testImage+"\nRUN echo VARIANT_OK > /variant.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := variantRef(dir)
	t.Cleanup(func() { exec.Command(runtimeName(), "rmi", ref).Run() })

	// Once with the CLI named, once for the whole project.
	for _, args := range [][]string{{"local", "variant", "destroy", "busy"}, {"local", "variant", "destroy"}} {
		out, err := runPTY(t, dir, data, "local", "variant", "build", "busy")
		if err != nil {
			t.Fatalf("variant build failed: %v\noutput:\n%s", err, out)
		}

		out, err = runPTY(t, dir, data, args...)
		if err != nil || !strings.Contains(out, "Removed variant of 'busy'") {
			t.Fatalf("dp %s failed: %v\noutput:\n%s", strings.Join(args, " "), err, out)
		}
		if img, _ := exec.Command(runtimeName(), "images", "-q", ref).Output(); len(bytes.TrimSpace(img)) != 0 {
			t.Fatalf("variant image %s still exists after destroy", ref)
		}
		out, code := run(t, data, "variants", "list")
		mustOK(t, "No tracked variants", out, code)
	}

	// The declaration survives, so the CLI asks for a rebuild instead of
	// falling back to the base image.
	out, err := runPTY(t, dir, data, "busy", "true")
	if err == nil || !strings.Contains(out, "has not been built yet") {
		t.Fatalf("expected a not-built error after destroy; got %v\noutput:\n%s", err, out)
	}

	// Nothing left: naming a CLI fails, the bare form is a no-op.
	out, err = runPTY(t, dir, data, "local", "variant", "destroy", "busy")
	if err == nil || !strings.Contains(out, "no variant of 'busy' is built") {
		t.Fatalf("expected destroy of a missing variant to fail; got %v\noutput:\n%s", err, out)
	}
	out, err = runPTY(t, dir, data, "local", "variant", "destroy")
	if err != nil || !strings.Contains(out, "No variants built for this project.") {
		t.Fatalf("bare destroy with nothing built failed: %v\noutput:\n%s", err, out)
	}
}

func TestContainer_VariantTrackedAndPruned(t *testing.T) {
	requireRuntime(t)
	data := t.TempDir()
	dir := t.TempDir()
	addBusybox(t, data)

	// Declare a variant of busy by hand rather than with 'extend', so the test
	// doesn't depend on base OS detection.
	if err := os.MkdirAll(filepath.Join(dir, ".dp", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "variants.json"), []byte(`{"variants": {"busy": {"build_file": ".dp/build/busy.Dockerfile"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dp", "build", "busy.Dockerfile"), []byte("FROM "+testImage+"\nRUN echo VARIANT_OK > /variant.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// dp local variant build busy
	out, err := runPTY(t, dir, data, "local", "variant", "build", "busy")
	if err != nil {
		t.Fatalf("variant build failed: %v\noutput:\n%s", err, out)
	}
	ref := variantRef(dir)
	t.Cleanup(func() { exec.Command(runtimeName(), "rmi", ref).Run() })

	// dp variants list: recorded by the build, never run yet
	out, code := run(t, data, "variants", "list")
	mustOK(t, dir, out, code)
	mustOK(t, "never", out, code)

	// dp busy cat /variant.txt: runs the variant and records the use
	out, err = runPTY(t, dir, data, "busy", "cat", "/variant.txt")
	if err != nil || !strings.Contains(out, "VARIANT_OK") {
		t.Fatalf("variant run failed: %v\noutput:\n%s", err, out)
	}
	out, code = run(t, data, "variants", "list")
	mustOK(t, "just now", out, code)

	// Rebuilding with a changed build file removes the image it replaces.
	oldID, _ := exec.Command(runtimeName(), "images", "-q", "--no-trunc", ref).Output()
	if err := os.WriteFile(filepath.Join(dir, ".dp", "build", "busy.Dockerfile"), []byte("FROM "+testImage+"\nRUN echo VARIANT_V2 > /variant.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runPTY(t, dir, data, "local", "variant", "build", "busy")
	if err != nil {
		t.Fatalf("variant rebuild failed: %v\noutput:\n%s", err, out)
	}
	if exec.Command(runtimeName(), "image", "inspect", string(bytes.TrimSpace(oldID))).Run() == nil {
		t.Fatalf("old variant image %s still exists after rebuild", bytes.TrimSpace(oldID))
	}

	// A recent, existing variant is kept.
	out, code = run(t, data, "variants", "prune", "--older-than", "30d", "--yes")
	mustOK(t, "Nothing to prune.", out, code)

	// Once its project is gone it is pruned whatever its age: image and record.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, data, "variants", "prune", "--older-than", "1000d", "--yes")
	mustOK(t, "Removed variant of 'busy'", out, code)

	out, code = run(t, data, "variants", "list")
	mustOK(t, "No tracked variants", out, code)
	if img, _ := exec.Command(runtimeName(), "images", "-q", ref).Output(); len(bytes.TrimSpace(img)) != 0 {
		t.Fatalf("variant image %s still exists after prune", ref)
	}
}
