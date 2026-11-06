package container

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// waylandHost is a Linux host whose Wayland socket exists (a plain file is
// enough: dp only checks that it's there).
func waylandHost(t *testing.T) AppHost {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wayland-1"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return AppHost{OS: "linux", UID: "1234", GID: "5678", RuntimeDir: dir, WaylandDisplay: "wayland-1"}
}

// hasSeq reports whether want appears in args as consecutive tokens.
func hasSeq(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// Docker passes tmpfs options to the kernel, so it can give the dir its owner.
func TestAppRunArgsDockerTmpfsOwner(t *testing.T) {
	args, _, err := AppRunArgs(RuntimeDocker, waylandHost(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasSeq(args, "--tmpfs", "/run/user/1234:rw,mode=0700,uid=1234,gid=5678,size=64m") {
		t.Errorf("missing owned tmpfs in %v", args)
	}
}

func TestAppRunArgsWayland(t *testing.T) {
	h := waylandHost(t)
	args, warnings, err := AppRunArgs(RuntimePodman, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	for _, want := range [][]string{
		{"-e", "WAYLAND_DISPLAY=wayland-1"},
		{"-e", "XDG_SESSION_TYPE=wayland"},
		{"-e", "XDG_RUNTIME_DIR=/run/user/1234"},
		{"--tmpfs", "/run/user/1234:rw,mode=1777,size=64m"},
		{"-v", filepath.Join(h.RuntimeDir, "wayland-1") + ":/run/user/1234/wayland-1"},
		{"--shm-size=1g"},
	} {
		if !hasSeq(args, want...) {
			t.Errorf("missing %v in %v", want, args)
		}
	}
	// Nothing the host didn't ask for.
	for _, unwanted := range []string{"label=disable", "/dev/dri", "nvidia.com/gpu=all", "--gpus"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("unexpected %q in %v", unwanted, args)
		}
	}
	for _, a := range args {
		if strings.HasPrefix(a, "TZ=") {
			t.Errorf("unknown time zone should pass no TZ, got %q", a)
		}
	}
}

func TestAppRunArgsTimeZone(t *testing.T) {
	h := waylandHost(t)
	h.TimeZone = "Europe/Stockholm"
	args, _, err := AppRunArgs(RuntimePodman, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasSeq(args, "-e", "TZ=Europe/Stockholm") {
		t.Errorf("missing TZ: %v", args)
	}
}

func TestDetectTimeZone(t *testing.T) {
	dir := t.TempDir()
	prevLocal, prevTZFile := localtimePath, timezonePath
	t.Cleanup(func() { localtimePath, timezonePath = prevLocal, prevTZFile })
	localtimePath = filepath.Join(dir, "localtime")
	timezonePath = filepath.Join(dir, "timezone")

	// Nothing to go on.
	t.Setenv("TZ", "")
	if got := detectTimeZone(); got != "" {
		t.Errorf("expected no zone, got %q", got)
	}

	// Debian-style file.
	os.WriteFile(timezonePath, []byte("America/New_York\n"), 0644)
	if got := detectTimeZone(); got != "America/New_York" {
		t.Errorf("from /etc/timezone: got %q", got)
	}

	// The symlink wins over the file (Fedora links relatively).
	if err := os.Symlink("../usr/share/zoneinfo/Europe/Stockholm", localtimePath); err != nil {
		t.Fatal(err)
	}
	if got := detectTimeZone(); got != "Europe/Stockholm" {
		t.Errorf("from /etc/localtime: got %q", got)
	}

	// An explicit TZ wins over both.
	t.Setenv("TZ", "Asia/Tokyo")
	if got := detectTimeZone(); got != "Asia/Tokyo" {
		t.Errorf("from $TZ: got %q", got)
	}
}

// WAYLAND_DISPLAY may be an absolute socket path; XDG_RUNTIME_DIR is then not needed.
func TestAppRunArgsAbsoluteDisplay(t *testing.T) {
	h := waylandHost(t)
	h.WaylandDisplay = filepath.Join(h.RuntimeDir, "wayland-1")
	h.RuntimeDir = ""
	args, _, err := AppRunArgs(RuntimeDocker, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasSeq(args, "-v", h.WaylandDisplay+":/run/user/1234/wayland-1") {
		t.Errorf("socket not mounted: %v", args)
	}
	if !hasSeq(args, "-e", "WAYLAND_DISPLAY=wayland-1") {
		t.Errorf("WAYLAND_DISPLAY should be the socket name inside the container: %v", args)
	}
}

func TestAppRunArgsErrors(t *testing.T) {
	cases := map[string]func(*AppHost){
		"not linux":      func(h *AppHost) { h.OS = "darwin" },
		"no display":     func(h *AppHost) { h.WaylandDisplay = "" },
		"no runtime dir": func(h *AppHost) { h.RuntimeDir = "" },
		"socket missing": func(h *AppHost) { h.WaylandDisplay = "wayland-9" },
	}
	for name, mutate := range cases {
		h := waylandHost(t)
		mutate(&h)
		if _, _, err := AppRunArgs(RuntimePodman, h); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// Every app runs hardened, whatever the host or runtime.
func TestAppRunArgsHardening(t *testing.T) {
	for _, rt := range []Runtime{RuntimeDocker, RuntimePodman} {
		args, _, err := AppRunArgs(rt, waylandHost(t))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", rt, err)
		}
		for _, want := range [][]string{
			{"--security-opt", "no-new-privileges"},
			{"--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE", "--cap-add=SETUID", "--cap-add=SETGID"},
			{"--pids-limit=16384"},
		} {
			if !hasSeq(args, want...) {
				t.Errorf("%s: missing %v in %v", rt, want, args)
			}
		}
	}
}

// The memory cap is 75% of host RAM, and skipped when RAM is unknown.
func TestAppRunArgsMemory(t *testing.T) {
	h := waylandHost(t)
	args, _, err := AppRunArgs(RuntimePodman, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--memory") {
			t.Errorf("unknown RAM should pass no limit, got %q", a)
		}
	}

	h.MemTotalKB = 16 * 1024 * 1024 // 16 GiB
	args, _, err = AppRunArgs(RuntimePodman, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Contains(args, "--memory=12288m") {
		t.Errorf("want --memory=12288m in %v", args)
	}
}

func TestMemTotalKB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if got := memTotalKB(path); got != 0 {
		t.Errorf("missing file: got %d", got)
	}
	os.WriteFile(path, []byte("MemTotal:       16078176 kB\nMemFree:         7746964 kB\n"), 0644)
	if got := memTotalKB(path); got != 16078176 {
		t.Errorf("got %d, want 16078176", got)
	}
}

func TestAppRunArgsHostFeatures(t *testing.T) {
	h := waylandHost(t)
	h.SELinux = true
	h.HasDRI = true
	args, _, err := AppRunArgs(RuntimePodman, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasSeq(args, "--security-opt", "label=disable") {
		t.Errorf("SELinux host should disable labelling: %v", args)
	}
	if !hasSeq(args, "--device", "/dev/dri") {
		t.Errorf("missing /dev/dri: %v", args)
	}
}

func TestAppRunArgsNvidia(t *testing.T) {
	cases := []struct {
		name     string
		runtime  Runtime
		cdi      bool
		toolkit  bool
		want     []string
		warnLike string
	}{
		{"podman with CDI", RuntimePodman, true, false, []string{"--device", "nvidia.com/gpu=all"}, ""},
		{"docker with toolkit", RuntimeDocker, false, true, []string{"--gpus", "all", "-e", "NVIDIA_DRIVER_CAPABILITIES=all"}, ""},
		{"podman without CDI", RuntimePodman, false, true, nil, "nvidia-ctk cdi generate"},
		{"docker without toolkit", RuntimeDocker, true, false, nil, "NVIDIA Container Toolkit"},
	}
	for _, tc := range cases {
		h := waylandHost(t)
		h.NVIDIA, h.NVIDIACDI, h.NVIDIAToolkit = true, tc.cdi, tc.toolkit
		args, warnings, err := AppRunArgs(tc.runtime, h)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if tc.want != nil && !hasSeq(args, tc.want...) {
			t.Errorf("%s: missing %v in %v", tc.name, tc.want, args)
		}
		if tc.warnLike == "" {
			if len(warnings) != 0 {
				t.Errorf("%s: unexpected warnings %v", tc.name, warnings)
			}
			continue
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], tc.warnLike) {
			t.Errorf("%s: expected a warning mentioning %q, got %v", tc.name, tc.warnLike, warnings)
		}
		if slices.Contains(args, "--gpus") || slices.Contains(args, "nvidia.com/gpu=all") {
			t.Errorf("%s: GPU flags without the toolkit would fail the run: %v", tc.name, args)
		}
	}
}

func TestHasNvidiaCDISpec(t *testing.T) {
	empty, withSpec, other := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(withSpec, "nvidia.yaml"), []byte("cdiVersion: 0.5.0\nkind: nvidia.com/gpu\n"), 0644)
	os.WriteFile(filepath.Join(other, "amd.yaml"), []byte("kind: amd.com/gpu\n"), 0644)
	os.WriteFile(filepath.Join(other, "notes.txt"), []byte("nvidia.com/gpu"), 0644)

	if hasNvidiaCDISpec([]string{empty, other, filepath.Join(empty, "missing")}) {
		t.Error("found a spec where there is none")
	}
	if !hasNvidiaCDISpec([]string{empty, withSpec}) {
		t.Error("missed the nvidia spec")
	}
}
