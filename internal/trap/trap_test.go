//go:build linux

package trap

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func write(t *testing.T, p, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, p string) {
	t.Helper()
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func newLayer(t *testing.T) Layer {
	t.Helper()
	l := Layer{Host: t.TempDir(), Rel: "x", Dir: t.TempDir()}
	mkdir(t, l.Upper())
	return l
}

func summary(changes []Change) []string {
	var out []string
	for _, c := range changes {
		out = append(out, c.String())
	}
	sort.Strings(out)
	return out
}

// The upper dirs here are built by hand the way overlayfs and fuse-overlayfs
// leave them, using the fuse-overlayfs whiteout files that need no privileges.
func TestDiffAndApply(t *testing.T) {
	l := newLayer(t)
	h, u := l.Host, l.Upper()

	write(t, filepath.Join(h, "edited.txt"), "one", 0644)
	write(t, filepath.Join(h, "touched.txt"), "same", 0644)
	write(t, filepath.Join(h, "chmod.sh"), "#!/bin/sh", 0644)
	write(t, filepath.Join(h, "gone.txt"), "bye", 0644)
	write(t, filepath.Join(h, "deldir", "y"), "y", 0644)
	write(t, filepath.Join(h, "dir", "kept"), "k", 0644)
	write(t, filepath.Join(h, "recreated", "old"), "old", 0644)
	symlink(t, "edited.txt", filepath.Join(h, "link"))

	write(t, filepath.Join(u, "edited.txt"), "two", 0644)
	write(t, filepath.Join(u, "touched.txt"), "same", 0644) // copied up, unchanged
	write(t, filepath.Join(u, "chmod.sh"), "#!/bin/sh", 0755)
	write(t, filepath.Join(u, "new.txt"), "new", 0644)
	write(t, filepath.Join(u, ".wh.gone.txt"), "", 0644)
	write(t, filepath.Join(u, ".wh.deldir"), "", 0644)
	write(t, filepath.Join(u, "dir", "added"), "a", 0644) // dir itself unchanged
	write(t, filepath.Join(u, "recreated", opaqueMarker), "", 0644)
	write(t, filepath.Join(u, "recreated", "fresh"), "f", 0644)
	write(t, filepath.Join(u, "newdir", "deep", "f"), "deep", 0644)
	write(t, filepath.Join(u, "suid"), "x", 0755|os.ModeSetuid)
	symlink(t, "new.txt", filepath.Join(u, "link"))
	if err := syscall.Mkfifo(filepath.Join(u, "fifo"), 0644); err != nil {
		t.Fatal(err)
	}

	changes, err := Diff(l)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	got := summary(changes)
	want := []string{
		"!  fifo (skipped: device, FIFO or socket)",
		"A  dir/added",
		"A  new.txt",
		"A  newdir/",
		"A  newdir/deep/",
		"A  newdir/deep/f",
		"A  recreated/fresh",
		"A  suid",
		"D  deldir",
		"D  gone.txt",
		"M  chmod.sh",
		"M  edited.txt",
		"M  link",
		"M  recreated/ (recreated)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Diff =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if errs := Apply(l, changes); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if read(t, filepath.Join(h, "edited.txt")) != "two" {
		t.Error("edited.txt not updated")
	}
	if info, _ := os.Stat(filepath.Join(h, "chmod.sh")); info.Mode().Perm() != 0755 {
		t.Errorf("chmod.sh mode = %v", info.Mode())
	}
	if exists(filepath.Join(h, "gone.txt")) || exists(filepath.Join(h, "deldir")) {
		t.Error("deleted entries still on the host")
	}
	if read(t, filepath.Join(h, "dir", "added")) != "a" || read(t, filepath.Join(h, "dir", "kept")) != "k" {
		t.Error("dir/ not merged")
	}
	if exists(filepath.Join(h, "recreated", "old")) || read(t, filepath.Join(h, "recreated", "fresh")) != "f" {
		t.Error("recreated/ should hold only what the tool put there")
	}
	if exists(filepath.Join(h, "recreated", opaqueMarker)) {
		t.Error("opaque marker leaked onto the host")
	}
	if read(t, filepath.Join(h, "newdir", "deep", "f")) != "deep" {
		t.Error("newdir/deep/f not added")
	}
	if target, _ := os.Readlink(filepath.Join(h, "link")); target != "new.txt" {
		t.Errorf("link -> %q", target)
	}
	if info, _ := os.Stat(filepath.Join(h, "suid")); info.Mode()&os.ModeSetuid != 0 || info.Mode().Perm() != 0755 {
		t.Errorf("suid mode = %v, want setuid dropped", info.Mode())
	}
	if exists(filepath.Join(h, "fifo")) {
		t.Error("fifo should not be created")
	}

	// Once committed, the layer and the host agree, except that an opaque dir
	// always reads as recreated.
	changes, err = Diff(l)
	if err != nil {
		t.Fatal(err)
	}
	got = summary(changes)
	want = []string{"!  fifo (skipped: device, FIFO or socket)", "A  recreated/fresh", "M  recreated/ (recreated)"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("re-Diff = %v, want %v", got, want)
	}
}

func TestDiffKernelWhiteout(t *testing.T) {
	l := newLayer(t)
	write(t, filepath.Join(l.Host, "gone"), "x", 0644)
	if err := unix.Mknod(filepath.Join(l.Upper(), "gone"), unix.S_IFCHR|0600, 0); err != nil {
		t.Skipf("cannot create a 0:0 device here: %v", err)
	}
	changes, err := Diff(l)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(changes); len(got) != 1 || got[0] != "D  gone" {
		t.Errorf("Diff = %v, want [D  gone]", got)
	}
}

func TestDiffOpaqueXattr(t *testing.T) {
	l := newLayer(t)
	write(t, filepath.Join(l.Host, "d", "old"), "x", 0644)
	mkdir(t, filepath.Join(l.Upper(), "d"))
	if err := unix.Lsetxattr(filepath.Join(l.Upper(), "d"), "user.overlay.opaque", []byte("y"), 0); err != nil {
		t.Skipf("user xattrs unsupported here: %v", err)
	}
	changes, err := Diff(l)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(changes); len(got) != 1 || got[0] != "M  d/ (recreated)" {
		t.Errorf("Diff = %v", got)
	}
}

// A dir the tool put where the host had a symlink out of the tree must not
// be written through.
func TestApplyReplacesEscapingSymlink(t *testing.T) {
	l := newLayer(t)
	outside := t.TempDir()
	symlink(t, outside, filepath.Join(l.Host, "esc"))
	write(t, filepath.Join(l.Upper(), "esc", "pwn"), "x", 0644)

	changes, err := Diff(l)
	if err != nil {
		t.Fatal(err)
	}
	if errs := Apply(l, changes); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if exists(filepath.Join(outside, "pwn")) {
		t.Fatal("wrote through the symlink to outside the tree")
	}
	if info, err := os.Lstat(filepath.Join(l.Host, "esc")); err != nil || !info.IsDir() {
		t.Errorf("esc should now be a real dir: %v %v", info, err)
	}
}

// Even if the host changes between Diff and Apply, the commit stays inside.
func TestApplyStaysInsideAfterHostSwap(t *testing.T) {
	l := newLayer(t)
	outside := t.TempDir()
	mkdir(t, filepath.Join(l.Host, "sub"))
	write(t, filepath.Join(l.Upper(), "sub", "f"), "x", 0644)

	changes, err := Diff(l)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(l.Host, "sub"))
	symlink(t, outside, filepath.Join(l.Host, "sub"))

	if errs := Apply(l, changes); len(errs) == 0 {
		t.Error("expected the escaping write to fail")
	}
	if exists(filepath.Join(outside, "f")) {
		t.Fatal("wrote outside the tree")
	}
}

func TestSessionOrphans(t *testing.T) {
	base := t.TempDir()
	host := t.TempDir()

	s, err := NewSession(base, true)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Add(host, "src")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(l.Upper()) || !exists(l.Work()) {
		t.Fatal("layer dirs not created")
	}

	// Held by a live run: not an orphan.
	if orphans, err := Orphans(base); err != nil || len(orphans) != 0 {
		t.Fatalf("Orphans = %v, %v; want none while locked", orphans, err)
	}

	// The run dies: its lock goes with it.
	s.unlock()
	orphans, err := Orphans(base)
	if err != nil || len(orphans) != 1 {
		t.Fatalf("Orphans = %v, %v; want one", orphans, err)
	}
	o := orphans[0]
	if !o.Commit || len(o.Layers) != 1 || o.Layers[0] != l {
		t.Errorf("orphan = %+v, want commit and layer %+v", o, l)
	}
	// Claimed by this scan, so a concurrent one skips it.
	if again, _ := Orphans(base); len(again) != 0 {
		t.Errorf("orphan offered twice")
	}
	if err := o.Remove(os.RemoveAll); err != nil || exists(o.Dir) {
		t.Errorf("Remove: %v", err)
	}
}
