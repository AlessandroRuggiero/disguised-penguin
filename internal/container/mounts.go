package container

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MountProtection is a single parsed --mp PATH:MODE spec, where Rel is the
// cleaned path relative to the workspace root and Mode is "ro", "rw", or "h".
type MountProtection struct {
	Rel  string
	Mode string
}

func (mp MountProtection) String() string {
	return fmt.Sprintf("%s:%s", mp.Rel, mp.Mode)
}

// ParseProtection parses a "--mp PATH:MODE" spec, validating the mode and
// confining the path to the workspace (no absolute paths or ".." escapes).
func ParseProtection(spec string) (MountProtection, error) {
	idx := strings.LastIndex(spec, ":")
	if idx <= 0 || idx == len(spec)-1 {
		return MountProtection{}, fmt.Errorf("invalid mount protection %q (expected PATH:MODE, e.g. .git:ro)", spec)
	}
	rawPath, mode := spec[:idx], spec[idx+1:]

	switch mode {
	case "ro", "rw", "h":
	default:
		return MountProtection{}, fmt.Errorf("invalid mount protection mode %q in %q (expected ro, rw, or h)", mode, spec)
	}

	rel := filepath.Clean(rawPath)
	if filepath.IsAbs(rel) {
		return MountProtection{}, fmt.Errorf("mount protection path %q must be relative to the workspace", rawPath)
	}
	if rel == "." {
		return MountProtection{}, fmt.Errorf("mount protection cannot target the workspace root %q", rawPath)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return MountProtection{}, fmt.Errorf("mount protection path %q must stay within the workspace", rawPath)
	}
	return MountProtection{Rel: rel, Mode: mode}, nil
}

// MountOpts builds the ":opt,opt" suffix for a -v bind mount. mode is "ro",
// "rw", or "" (default). On SELinux hosts a "z" relabel option is added so the
// container can access the host path (see the base /workspace mount for why).
func MountOpts(mode string, selinux bool) string {
	var opts []string
	if mode != "" {
		opts = append(opts, mode)
	}
	if selinux {
		opts = append(opts, "z")
	}
	if len(opts) == 0 {
		return ""
	}
	return ":" + strings.Join(opts, ",")
}

// HidePlaceholders provides the empty dir and file bind mounted over targets
// to hide them ("h" mode). Their contents never change, so they live at a
// fixed path under dir and are reused across runs. dir must be a host path the
// runtime shares; the app data dir sits under $HOME, shared by default.
type HidePlaceholders struct {
	dir string
}

func NewHidePlaceholders(dir string) *HidePlaceholders {
	return &HidePlaceholders{dir: dir}
}

// Get returns an empty placeholder path matching the target type (dir or
// file), creating it on demand. Creation is idempotent, so it recreates a
// deleted placeholder and stays safe across concurrent dp runs.
func (h *HidePlaceholders) Get(isDir bool) (string, error) {
	if err := os.MkdirAll(h.dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create hide placeholder: %w", err)
	}
	if isDir {
		p := filepath.Join(h.dir, "dir")
		if err := os.Mkdir(p, 0755); err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("failed to create hide placeholder: %w", err)
		}
		return p, nil
	}
	p := filepath.Join(h.dir, "file")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.WriteFile(p, nil, 0644); err != nil {
			return "", fmt.Errorf("failed to create hide placeholder: %w", err)
		}
	}
	return p, nil
}
