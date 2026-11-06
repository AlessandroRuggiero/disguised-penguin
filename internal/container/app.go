package container

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
)

// AppHost is what dp needs to know about the host to run a graphical app or
// give a CLI CUDA.
// DetectAppHost fills it in; tests build it by hand.
type AppHost struct {
	OS             string
	UID, GID       string // what dp passes as PUID/PGID: the user the entrypoint runs the app as
	RuntimeDir     string // host XDG_RUNTIME_DIR
	WaylandDisplay string // host WAYLAND_DISPLAY: a socket name in RuntimeDir, or an absolute path
	TimeZone       string // host TZ value, e.g. Europe/Stockholm; empty if unknown
	SELinux        bool
	HasDRI         bool   // /dev/dri exists
	NVIDIA         bool   // the NVIDIA kernel driver is loaded
	NVIDIACDI      bool   // a CDI spec for nvidia.com/gpu exists (what podman uses)
	NVIDIAToolkit  bool   // the NVIDIA container toolkit is installed (what docker --gpus uses)
	MemTotalKB     uint64 // host RAM from /proc/meminfo; 0 if unknown
}

// Where the host's time zone is read from, when TZ isn't set.
var (
	localtimePath = "/etc/localtime"
	timezonePath  = "/etc/timezone" // Debian/Ubuntu
)

// Where the host's RAM is read from.
var meminfoPath = "/proc/meminfo"

// Where nvidia-ctk writes CDI specs (static, and the nvidia-cdi-refresh service).
var cdiSpecDirs = []string{"/etc/cdi", "/var/run/cdi"}

func DetectAppHost(selinux bool) AppHost {
	h := AppHost{
		OS:             goruntime.GOOS,
		UID:            strconv.Itoa(os.Getuid()),
		GID:            strconv.Itoa(os.Getgid()),
		RuntimeDir:     os.Getenv("XDG_RUNTIME_DIR"),
		WaylandDisplay: os.Getenv("WAYLAND_DISPLAY"),
		SELinux:        selinux,
	}
	// The same lookup as PUID/PGID, so the runtime dir's owner matches them.
	if u, err := user.Current(); err == nil {
		h.UID, h.GID = u.Uid, u.Gid
	}
	if h.OS != "linux" {
		return h
	}
	h.TimeZone = detectTimeZone()
	h.MemTotalKB = memTotalKB(meminfoPath)
	_, err := os.Stat("/dev/dri")
	h.HasDRI = err == nil
	_, err = os.Stat("/proc/driver/nvidia/version")
	h.NVIDIA = err == nil
	if h.NVIDIA {
		h.NVIDIACDI = hasNvidiaCDISpec(cdiSpecDirs)
		for _, bin := range []string{"nvidia-container-runtime-hook", "nvidia-container-runtime"} {
			if _, err := lookPath(bin); err == nil {
				h.NVIDIAToolkit = true
				break
			}
		}
	}
	return h
}

// memTotalKB returns the MemTotal line of a meminfo file, in kB; 0 if unreadable.
func memTotalKB(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			kb, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			return kb
		}
	}
	return 0
}

// detectTimeZone returns the host's time zone as a TZ value: $TZ itself,
// else the IANA name /etc/localtime links to, else /etc/timezone.
func detectTimeZone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if target, err := os.Readlink(localtimePath); err == nil {
		if _, name, ok := strings.Cut(target, "zoneinfo/"); ok && name != "" {
			return name
		}
	}
	if data, err := os.ReadFile(timezonePath); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func hasNvidiaCDISpec(dirs []string) bool {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			if e.IsDir() || (ext != ".yaml" && ext != ".yml" && ext != ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil && strings.Contains(string(data), "nvidia.com/gpu") {
				return true
			}
		}
	}
	return false
}

// AppRunArgs returns the runtime flags that let a graphical app open a window
// on the host's Wayland session and render on its GPU (/dev/dri).
// Warnings describe what is missing but doesn't stop the app from starting.
func AppRunArgs(runtime Runtime, h AppHost) (args []string, warnings []string, err error) {
	if h.OS != "linux" {
		return nil, nil, fmt.Errorf("graphical apps are only supported on Linux")
	}

	// Only the compositor socket is shared, not the rest of the host's
	// /run/user. The container gets its own writable XDG_RUNTIME_DIR on a
	// tmpfs (apps put their IPC sockets there) with the socket bound into it.
	if h.WaylandDisplay == "" {
		return nil, nil, fmt.Errorf("no Wayland session found (WAYLAND_DISPLAY is not set)")
	}
	socket := h.WaylandDisplay
	if !filepath.IsAbs(socket) {
		if h.RuntimeDir == "" {
			return nil, nil, fmt.Errorf("cannot locate the Wayland socket: XDG_RUNTIME_DIR is not set")
		}
		socket = filepath.Join(h.RuntimeDir, socket)
	}
	if _, err := os.Stat(socket); err != nil {
		return nil, nil, fmt.Errorf("Wayland socket %s not found: %w", socket, err)
	}
	name := filepath.Base(socket)
	// The entrypoint runs the app as the host user's UID (PUID), so this is
	// the runtime dir that user expects.
	runDir := "/run/user/" + h.UID
	// Owned by that user and private to it, as the XDG spec requires:
	// D-Bus and Qt complain about a root-owned, world-writable one.
	tmpfsOpts := "rw,mode=0700,uid=" + h.UID + ",gid=" + h.GID + ",size=64m"
	if runtime == RuntimePodman {
		// Podman rejects uid=/gid= ("unknown mount option"), so the best it
		// can do is a sticky world-writable dir the user can still write to.
		tmpfsOpts = "rw,mode=1777,size=64m"
	}
	args = append(args,
		"-e", "WAYLAND_DISPLAY="+name,
		// With no X DISPLAY to fall back to, this tells GTK, Qt and Electron
		// to go straight to Wayland.
		"-e", "XDG_SESSION_TYPE=wayland",
		"-e", "XDG_RUNTIME_DIR="+runDir,
		"--tmpfs", runDir+":"+tmpfsOpts,
		"-v", socket+":"+runDir+"/"+name,
	)

	// The host socket is labelled user_tmp_t, which a confined container
	// can't connect to, and relabelling it would affect every other client.
	if h.SELinux {
		args = append(args, "--security-opt", "label=disable")
	}

	// Otherwise the app shows times in UTC. A name rather than a mounted
	// /etc/localtime: ICU (Chromium/Electron) resolves names on its own, and
	// mounting over the image's /etc/localtime symlink would overwrite the
	// zone file it points to.
	if h.TimeZone != "" {
		args = append(args, "-e", "TZ="+h.TimeZone)
	}

	// Chromium/Electron and CUDA data loaders exhaust the 64MB default.
	args = append(args, "--shm-size=1g")

	// An app sees the display, so lock down what's left. Keep only the caps
	// the entrypoint needs to remap penguin and drop to it, and block setuid
	// escalation back to root. A package can still --cap-add what it needs or
	// pass no-new-privileges=false: both runtimes let a later flag win.
	args = append(args,
		"--security-opt", "no-new-privileges",
		"--cap-drop=ALL",
		"--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE", "--cap-add=SETUID", "--cap-add=SETGID",
		// Counts threads, not processes. Room for parallel builds on many-core
		// machines (podman's own default of 2048 is tight), yet a fork bomb
		// still can't use up the host's threads (docker sets no limit at all).
		"--pids-limit=16384",
	)

	// Not sized to the app but to the machine: a leak gets OOM-killed inside
	// the container while the compositor and the rest of the session keep a
	// quarter of the RAM.
	if h.MemTotalKB > 0 {
		args = append(args, fmt.Sprintf("--memory=%dm", h.MemTotalKB*3/4/1024))
	}

	// Hardware-accelerated rendering (Mesa: Intel, AMD, nouveau). The NVIDIA
	// GPU is opt-in (CUDARunArgs): most apps don't need it, and on a hybrid
	// laptop opening it wakes the dGPU and costs battery.
	if h.HasDRI {
		args = append(args, "--device", "/dev/dri")
	}
	return args, warnings, nil
}

// CUDARunArgs returns the runtime flags that give a CLI the host's NVIDIA
// GPUs, CUDA included. Warnings say why it runs without them.
func CUDARunArgs(runtime Runtime, h AppHost) (args []string, warnings []string) {
	if !h.NVIDIA {
		return nil, []string{"cuda is set but no NVIDIA driver is loaded, so it runs without CUDA"}
	}
	// NVIDIA's userspace driver must match the host kernel module, so the
	// container toolkit injects the devices and libraries (CUDA included)
	// rather than the image shipping its own.
	switch {
	case runtime == RuntimePodman && h.NVIDIACDI:
		args = append(args, "--device", "nvidia.com/gpu=all")
	case runtime == RuntimeDocker && h.NVIDIAToolkit:
		// --gpus only grants compute and utility by default; graphics lets an
		// app render on the GPU too.
		args = append(args, "--gpus", "all", "-e", "NVIDIA_DRIVER_CAPABILITIES=all")
	case runtime == RuntimePodman:
		warnings = append(warnings, "an NVIDIA GPU is present but podman has no CDI spec for it, so it runs without CUDA. Generate one with: sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml")
	default:
		warnings = append(warnings, "an NVIDIA GPU is present but the NVIDIA Container Toolkit is not installed, so it runs without CUDA")
	}
	return args, warnings
}
