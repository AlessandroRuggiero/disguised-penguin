// Package trap holds the layers behind write-trapped ("wt") mounts. Podman
// overlays a wt directory with an upper dir dp owns, so the tool's writes land
// there instead of on the host; after the run dp diffs each layer against the
// host and commits or discards it.
package trap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	sessionFile = "session.json"
	lockFile    = "lock"
	runPrefix   = "run-"
)

// Layer is the overlay upper/work pair over one wt directory.
type Layer struct {
	Host string `json:"host"` // host directory the layer sits on
	Rel  string `json:"rel"`  // workspace-relative path, for messages
	Dir  string `json:"dir"`  // holds upper/ and work/
}

func (l Layer) Upper() string { return filepath.Join(l.Dir, "upper") }
func (l Layer) Work() string  { return filepath.Join(l.Dir, "work") }

// Session is one run's layers. It lives in its own dir under the overlays
// base and holds an exclusive lock for as long as the run is alive, so a dir
// whose lock can be taken belongs to a dp that died before settling it.
type Session struct {
	Dir    string  `json:"-"`
	Commit bool    `json:"commit"` // --commit was passed to the run
	Layers []Layer `json:"layers"`
	lock   *os.File
}

// NewSession creates a locked session under base. It is built under a
// temporary name and renamed into place only once locked, so an orphan scan
// running concurrently never mistakes it for a dead run.
func NewSession(base string, commit bool) (*Session, error) {
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, fmt.Errorf("failed to create overlays dir: %w", err)
	}
	tmp, err := os.MkdirTemp(base, ".new-")
	if err != nil {
		return nil, fmt.Errorf("failed to create overlay session: %w", err)
	}
	s := &Session{Dir: tmp, Commit: commit}
	if s.lock, err = lock(filepath.Join(tmp, lockFile)); err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("failed to lock overlay session: %w", err)
	}
	if err := s.save(); err != nil {
		s.unlock()
		os.RemoveAll(tmp)
		return nil, err
	}
	dir := filepath.Join(base, runPrefix+strings.TrimPrefix(filepath.Base(tmp), ".new-"))
	if err := os.Rename(tmp, dir); err != nil {
		s.unlock()
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("failed to create overlay session: %w", err)
	}
	s.Dir = dir
	return s, nil
}

// Add creates a layer over the host directory and records it in the session.
func (s *Session) Add(host, rel string) (Layer, error) {
	info, err := os.Stat(host)
	if err != nil {
		return Layer{}, err
	}
	l := Layer{Host: host, Rel: rel, Dir: filepath.Join(s.Dir, strconv.Itoa(len(s.Layers)))}
	// The overlay root takes the upper root's attributes, so mirror the host's.
	if err := os.MkdirAll(l.Upper(), 0700); err != nil {
		return Layer{}, fmt.Errorf("failed to create overlay layer: %w", err)
	}
	if err := os.Chmod(l.Upper(), info.Mode().Perm()); err != nil {
		return Layer{}, fmt.Errorf("failed to create overlay layer: %w", err)
	}
	if err := os.MkdirAll(l.Work(), 0700); err != nil {
		return Layer{}, fmt.Errorf("failed to create overlay layer: %w", err)
	}
	s.Layers = append(s.Layers, l)
	return l, s.save()
}

func (s *Session) save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, sessionFile), data, 0600); err != nil {
		return fmt.Errorf("failed to save overlay session: %w", err)
	}
	return nil
}

func (s *Session) unlock() {
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
}

// Remove releases the session and deletes its layers. remove deletes a dir
// tree; podman can leave entries owned by subordinate UIDs that only
// `podman unshare` may delete, so the caller supplies how.
func (s *Session) Remove(remove func(string) error) error {
	s.unlock()
	return remove(s.Dir)
}

// Orphans returns the sessions under base left behind by runs that died,
// each locked by the caller until it is removed.
func Orphans(base string) ([]*Session, error) {
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read overlays dir: %w", err)
	}
	var orphans []*Session
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), runPrefix) {
			continue
		}
		dir := filepath.Join(base, e.Name())
		f, err := lock(filepath.Join(dir, lockFile))
		if err != nil {
			continue // a live run holds it
		}
		s := &Session{Dir: dir, lock: f}
		// An unreadable session has no layers to offer, but is still removed.
		if data, err := os.ReadFile(filepath.Join(dir, sessionFile)); err == nil {
			json.Unmarshal(data, s)
		}
		orphans = append(orphans, s)
	}
	return orphans, nil
}
