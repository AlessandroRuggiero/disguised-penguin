package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"
)

// localMountsFile declares this project's .dp/ folder mounts.
const localMountsFile = ".dp/mounts.json"

// localMount is a parsed .dp/mounts.json entry. Source (optional, under .dp/)
// is bound at Dest with Mode "ro", "rw", or "h".
type localMount struct {
	Source string
	Dest   string
	Mode   string
}

// cleanLocalRel cleans a config path and confines it: no absolute paths,
// no ".." escapes, not the root itself.
func cleanLocalRel(p string) (string, error) {
	rel := path.Clean(p)
	if path.IsAbs(rel) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("%q must be relative", p)
	}
	if rel == "." {
		return "", fmt.Errorf("%q cannot be the directory root", p)
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%q must stay within the directory", p)
	}
	return rel, nil
}

// loadLocalMounts reads dir/.dp/mounts.json. A missing file yields no mounts,
// an invalid one an error. Entries are sorted by destination for determinism.
func loadLocalMounts(dir string) ([]localMount, error) {
	p := filepath.Join(dir, filepath.FromSlash(localMountsFile))
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", p, err)
	}

	var config models.LocalMountConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", p, err)
	}

	mounts := make([]localMount, 0, len(config.Mounts))
	for i, m := range config.Mounts {
		if m.Mode != "ro" && m.Mode != "rw" && m.Mode != "h" {
			return nil, fmt.Errorf("invalid local mount mode %q at index %d (expected \"ro\", \"rw\", or \"h\")", m.Mode, i)
		}
		if m.Path == "" {
			return nil, fmt.Errorf("local mount at index %d is missing \"path\"", i)
		}
		dest, err := cleanLocalRel(m.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid local mount path %s", err)
		}
		var src string
		if m.Source != "" {
			if m.Mode == "h" {
				return nil, fmt.Errorf("local mount %q: a \"source\" cannot be combined with mode \"h\"", m.Path)
			}
			src, err = cleanLocalRel(m.Source)
			if err != nil {
				return nil, fmt.Errorf("invalid local mount source %s", err)
			}
		}
		mounts = append(mounts, localMount{Source: src, Dest: dest, Mode: m.Mode})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].Dest < mounts[j].Dest })
	return mounts, nil
}

// addPathOverlay overlays the workspace path rel with Mode "ro", "rw", or "h".
// A missing host path is skipped; label names the entry in error messages.
func addPathOverlay(overlays map[string]string, cwd string, selinux bool, hides *container.HidePlaceholders, rel, mode, label string) error {
	hostPath := filepath.Join(cwd, filepath.FromSlash(rel))
	info, err := os.Stat(hostPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: cannot access %s: %w", label, rel, err)
	}
	dest := path.Join("/workspace", filepath.ToSlash(rel))
	switch mode {
	case "ro", "rw":
		overlays[dest] = hostPath + ":" + dest + container.MountOpts(mode, selinux)
	case "h":
		// Hide the host path by binding an empty placeholder over it.
		empty, err := hides.Get(info.IsDir())
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		overlays[dest] = empty + ":" + dest + container.MountOpts("ro", selinux)
	}
	return nil
}

// addProtection resolves a "PATH:MODE" protection spec into an overlay.
func addProtection(overlays map[string]string, cwd string, selinux bool, hides *container.HidePlaceholders, spec string) error {
	prot, err := container.ParseProtection(spec)
	if err != nil {
		return err
	}
	return addPathOverlay(overlays, cwd, selinux, hides, prot.Rel, prot.Mode, fmt.Sprintf("mount protection %q", spec))
}

// addLocalMount resolves a .dp/mounts.json entry. Without a source it overlays
// the workspace path; with one it binds .dp/<source> at Dest and hides the source.
func addLocalMount(overlays map[string]string, cwd string, selinux bool, hides *container.HidePlaceholders, m localMount) error {
	if m.Source == "" {
		return addPathOverlay(overlays, cwd, selinux, hides, m.Dest, m.Mode, fmt.Sprintf("local mount %q", m.Dest))
	}

	hostSource := filepath.Join(cwd, ".dp", filepath.FromSlash(m.Source))
	info, err := os.Stat(hostSource)
	if err != nil {
		return fmt.Errorf("local mount %q: cannot access .dp/%s: %w", m.Source, m.Source, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local mount %q: .dp/%s is not a directory", m.Source, m.Source)
	}

	// Hide the source under .dp so it is only reachable at the destination.
	hideDest := path.Join("/workspace/.dp", m.Source)
	empty, err := hides.Get(true)
	if err != nil {
		return fmt.Errorf("local mount %q: %w", m.Source, err)
	}
	overlays[hideDest] = empty + ":" + hideDest + container.MountOpts("ro", selinux)

	// Bind the real content at the destination.
	dest := path.Join("/workspace", m.Dest)
	overlays[dest] = hostSource + ":" + dest + container.MountOpts(m.Mode, selinux)
	return nil
}

// buildWorkspaceOverlays assembles the overlay mounts on top of /workspace.
// Later tiers win per destination: workspace, then .dp/, then args.
func buildWorkspaceOverlays(cwd string, selinux bool, hides *container.HidePlaceholders, workspaceSpecs []string, localMounts []localMount, argSpecs []string) ([]string, error) {
	overlays := map[string]string{}

	// .dp/ is dp's control plane, consumed on the host, so default it to read only.
	// Seeded first (lowest priority) so a mount or arg can reopen it; skipped if absent.
	if err := addPathOverlay(overlays, cwd, selinux, hides, ".dp", "ro", "workspace .dp"); err != nil {
		return nil, err
	}

	for _, spec := range workspaceSpecs {
		if err := addProtection(overlays, cwd, selinux, hides, spec); err != nil {
			return nil, err
		}
	}
	for _, m := range localMounts {
		if err := addLocalMount(overlays, cwd, selinux, hides, m); err != nil {
			return nil, err
		}
	}
	for _, spec := range argSpecs {
		if err := addProtection(overlays, cwd, selinux, hides, spec); err != nil {
			return nil, err
		}
	}

	dests := make([]string, 0, len(overlays))
	for d := range overlays {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	args := make([]string, len(dests))
	for i, d := range dests {
		args[i] = overlays[d]
	}
	return args, nil
}
