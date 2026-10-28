//go:build !linux

package trap

import (
	"io/fs"
	"os"
)

// wt mounts need rootless podman on Linux, so elsewhere there are no layers
// to read; these only keep the package building.

var opaqueXattrs, redirectXattrs, metacopyXattrs []string

func isWhiteout(fs.FileInfo) bool { return false }

func hasXattr(string, []string, string) bool { return false }

func lock(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0600)
}
