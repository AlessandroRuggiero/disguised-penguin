package trap

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// fuse-overlayfs falls back to these when it cannot create 0:0 devices.
const (
	whiteoutPrefix = ".wh."
	opaqueMarker   = ".wh..wh..opq"
)

type Op int

const (
	Add Op = iota
	Modify
	Delete
	Skip
)

// Change is one difference between a layer and its host directory.
type Change struct {
	Op      Op
	Path    string      // slash-separated, relative to the layer root
	Mode    fs.FileMode // type and permissions of the upper entry
	Replace bool        // dir only: the host entry is wiped and recreated
	Meta    bool        // file only: overlay copied up the metadata alone
	Reason  string      // why a Skip is skipped
}

func (c Change) String() string {
	p := c.Path
	if c.Mode.IsDir() && c.Op != Delete {
		p += "/"
	}
	switch c.Op {
	case Add:
		return "A  " + p
	case Modify:
		if c.Replace {
			return "M  " + p + " (recreated)"
		}
		return "M  " + p
	case Delete:
		return "D  " + p
	default:
		return "!  " + p + " (skipped: " + c.Reason + ")"
	}
}

// Diff lists what committing the layer would change on the host. Entries the
// overlay copied up without changing them are left out.
func Diff(l Layer) ([]Change, error) {
	root, err := os.OpenRoot(l.Host)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	upper := l.Upper()
	var changes []Change
	deleted := map[string]bool{}
	// Dirs added or recreated by the tool: nothing below them exists on the host.
	fresh := map[string]bool{}

	del := func(rel string) error {
		if deleted[rel] {
			return nil
		}
		info, err := lstat(root, rel)
		if err != nil || info == nil {
			return err
		}
		deleted[rel] = true
		changes = append(changes, Change{Op: Delete, Path: rel, Mode: info.Mode()})
		return nil
	}

	err = filepath.WalkDir(upper, func(p string, d fs.DirEntry, err error) error {
		if p == upper {
			return err
		}
		r, _ := filepath.Rel(upper, p)
		rel := filepath.ToSlash(r)
		if err != nil {
			changes = append(changes, Change{Op: Skip, Path: rel, Reason: err.Error()})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		parentFresh := fresh[path.Dir(rel)]

		name := d.Name()
		if name == opaqueMarker {
			return nil // read along with its dir
		}
		if strings.HasPrefix(name, whiteoutPrefix) {
			if parentFresh {
				return nil
			}
			return del(path.Join(path.Dir(rel), strings.TrimPrefix(name, whiteoutPrefix)))
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if isWhiteout(info) {
			if parentFresh {
				return nil
			}
			return del(rel)
		}

		var host fs.FileInfo
		if !parentFresh {
			if host, err = lstat(root, rel); err != nil {
				return err
			}
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if hasXattr(p, redirectXattrs, "") {
				changes = append(changes, Change{Op: Skip, Path: rel, Mode: mode, Reason: "renamed directory, which overlay redirects"})
				return fs.SkipDir
			}
			opaque := hasXattr(p, opaqueXattrs, "y")
			if _, err := os.Lstat(filepath.Join(p, opaqueMarker)); err == nil {
				opaque = true
			}
			switch {
			case host == nil:
				fresh[rel] = true
				changes = append(changes, Change{Op: Add, Path: rel, Mode: mode})
			case !host.IsDir() || opaque:
				fresh[rel] = true
				changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode, Replace: true})
			case host.Mode().Perm() != mode.Perm():
				changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode})
			}

		case mode.IsRegular():
			if hasXattr(p, metacopyXattrs, "") {
				// The data still lives in the lower layer, i.e. the host file.
				switch {
				case host == nil || !host.Mode().IsRegular():
					changes = append(changes, Change{Op: Skip, Path: rel, Mode: mode, Reason: "metadata-only copy with no data"})
				case host.Mode().Perm() != mode.Perm():
					changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode, Meta: true})
				}
				return nil
			}
			switch {
			case host == nil:
				changes = append(changes, Change{Op: Add, Path: rel, Mode: mode})
			case !host.Mode().IsRegular() || host.Mode().Perm() != mode.Perm():
				changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode})
			default:
				same, err := sameContent(root, rel, p, host.Size(), info.Size())
				if err != nil {
					return err
				}
				if !same {
					changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode})
				}
			}

		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			switch {
			case host == nil:
				changes = append(changes, Change{Op: Add, Path: rel, Mode: mode})
			case host.Mode()&fs.ModeSymlink == 0:
				changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode})
			default:
				if hostTarget, err := root.Readlink(rel); err != nil || hostTarget != target {
					changes = append(changes, Change{Op: Modify, Path: rel, Mode: mode})
				}
			}

		default:
			changes = append(changes, Change{Op: Skip, Path: rel, Mode: mode, Reason: "device, FIFO or socket"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// lstat returns nil, nil when rel is absent from the host.
func lstat(root *os.Root, rel string) (fs.FileInfo, error) {
	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	return info, err
}

func sameContent(root *os.Root, rel, upperPath string, hostSize, upperSize int64) (bool, error) {
	if hostSize != upperSize {
		return false, nil
	}
	a, err := root.Open(rel)
	if err != nil {
		return false, err
	}
	defer a.Close()
	b, err := os.Open(upperPath)
	if err != nil {
		return false, err
	}
	defer b.Close()

	bufA, bufB := make([]byte, 64*1024), make([]byte, 64*1024)
	for {
		n, errA := io.ReadFull(a, bufA)
		m, errB := io.ReadFull(b, bufB)
		if n != m || !bytes.Equal(bufA[:n], bufB[:m]) {
			return false, nil
		}
		if errA == io.EOF || errA == io.ErrUnexpectedEOF {
			return errB == io.EOF || errB == io.ErrUnexpectedEOF, nil
		}
		if errA != nil {
			return false, errA
		}
		if errB != nil {
			return false, errB
		}
	}
}

// Apply commits changes from Diff onto the host. Every host operation goes
// through an os.Root on the layer's host dir, so no path, symlink included,
// can reach outside it. Only dirs, regular files and symlinks are created,
// and setuid/setgid/sticky bits are dropped. A failed entry does not stop
// the rest; its error is returned and anything below a failed dir is skipped.
func Apply(l Layer, changes []Change) []error {
	root, err := os.OpenRoot(l.Host)
	if err != nil {
		return []error{err}
	}
	defer root.Close()

	var errs []error
	var failed []string
	for _, c := range changes {
		if c.Op == Skip || under(c.Path, failed) {
			continue
		}
		if err := apply(root, l.Upper(), c); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Path, err))
			if c.Mode.IsDir() {
				failed = append(failed, c.Path)
			}
		}
	}
	return errs
}

func under(p string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

func apply(root *os.Root, upper string, c Change) error {
	rel := filepath.FromSlash(c.Path)
	src := filepath.Join(upper, rel)
	perm := c.Mode.Perm()

	if c.Op == Delete {
		return root.RemoveAll(rel)
	}

	host, err := lstat(root, rel)
	if err != nil {
		return err
	}

	switch {
	case c.Mode.IsDir():
		if host != nil && (c.Replace || !host.IsDir()) {
			if err := root.RemoveAll(rel); err != nil {
				return err
			}
			host = nil
		}
		if host == nil {
			if err := root.Mkdir(rel, perm); err != nil {
				return err
			}
		}
		return root.Chmod(rel, perm)

	case c.Mode.IsRegular() && c.Meta:
		if host == nil || !host.Mode().IsRegular() {
			return fmt.Errorf("host file is gone")
		}
		return root.Chmod(rel, perm)

	case c.Mode.IsRegular():
		return replace(root, rel, host, func(tmp string) error {
			in, err := os.Open(src)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			return root.Chmod(tmp, perm)
		})

	case c.Mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return replace(root, rel, host, func(tmp string) error {
			return root.Symlink(target, tmp)
		})
	}
	return fmt.Errorf("unsupported file type %s", c.Mode.Type())
}

// replace builds the new entry beside rel and renames it over, so a host
// symlink at rel is swapped out rather than written through.
func replace(root *os.Root, rel string, host fs.FileInfo, build func(tmp string) error) error {
	suffix := make([]byte, 6)
	rand.Read(suffix)
	tmp := filepath.Join(filepath.Dir(rel), ".dp-wt-"+hex.EncodeToString(suffix))
	if err := build(tmp); err != nil {
		root.Remove(tmp)
		return err
	}
	if host != nil && host.IsDir() {
		if err := root.RemoveAll(rel); err != nil {
			root.Remove(tmp)
			return err
		}
	}
	if err := root.Rename(tmp, rel); err != nil {
		root.Remove(tmp)
		return err
	}
	return nil
}
