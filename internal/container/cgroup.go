package container

import (
	"fmt"
	"strings"
)

// AppCgroupFlags places a podman container where desktop system monitors look
// for applications: a scope under app-dp.<cli>.slice, with the processes
// directly in it (crun otherwise nests them in a "container" sub-cgroup).
func AppCgroupFlags(cliName string) []string {
	return []string{
		"--cgroup-parent=app-dp." + escapeUnitName(cliName) + ".slice",
		"--annotation", "run.oci.systemd.subgroup=",
	}
}

// escapeUnitName escapes like systemd-escape: "-" would nest slices.
func escapeUnitName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == ':':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}
