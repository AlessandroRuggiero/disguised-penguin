package trap

import (
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Kernel overlayfs uses trusted.overlay.* when mounted by real root and
// user.overlay.* (userxattr) in a user namespace; fuse-overlayfs has its own.
var (
	opaqueXattrs   = []string{"trusted.overlay.opaque", "user.overlay.opaque", "user.fuseoverlayfs.opaque"}
	redirectXattrs = []string{"trusted.overlay.redirect", "user.overlay.redirect"}
	metacopyXattrs = []string{"trusted.overlay.metacopy", "user.overlay.metacopy"}
)

// isWhiteout reports an overlayfs whiteout: a 0:0 character device.
func isWhiteout(info fs.FileInfo) bool {
	if info.Mode()&fs.ModeCharDevice == 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Rdev == 0
}

// hasXattr reports whether p carries any of names, optionally with value want.
func hasXattr(p string, names []string, want string) bool {
	buf := make([]byte, 256)
	for _, name := range names {
		n, err := unix.Lgetxattr(p, name, buf)
		if err != nil && err != unix.ERANGE {
			continue
		}
		if want == "" || (err == nil && string(buf[:n]) == want) {
			return true
		}
	}
	return false
}

// lock takes an exclusive, non-blocking lock on p, creating it. The kernel
// drops it when the process dies, which is what marks a session orphaned.
func lock(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
