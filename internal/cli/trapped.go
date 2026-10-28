package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"disguised-penguin/internal/db"
	"disguised-penguin/internal/models"
	"disguised-penguin/internal/trap"
)

// Longer change lists are cut, ending with a count of the rest.
const maxListedChanges = 20

// overlaysDir holds the wt layers, beside the database.
func overlaysDir() (string, error) {
	dbPath, err := db.GetDBPath()
	if err != nil {
		return "", fmt.Errorf("failed to resolve data directory: %w", err)
	}
	return filepath.Join(filepath.Dir(dbPath), "overlays"), nil
}

// removeTree deletes a session dir. Writes made as root in a rootless
// container are owned by a subordinate UID, which only `podman unshare` (root
// in dp's user namespace) can delete.
func removeTree(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	out, err := exec.Command("podman", "unshare", "rm", "-rf", dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove %s: %v: %s", dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// settleSession commits or discards each layer per the wt_commit policy, then
// deletes the session. tty is whether there is a terminal to ask on.
func settleSession(s *trap.Session, policy models.WtCommit, tty bool) {
	for _, l := range s.Layers {
		settleLayer(l, policy, s.Commit, tty)
	}
	if err := s.Remove(removeTree); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
}

func settleLayer(l trap.Layer, policy models.WtCommit, commitFlag, tty bool) {
	if _, err := os.Stat(l.Host); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: discarded the trapped writes to %s: %v\n", l.Rel, err)
		return
	}
	changes, err := trap.Diff(l)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: discarded the trapped writes to %s, they could not be read: %v\n", l.Rel, err)
		return
	}
	n := 0
	for _, c := range changes {
		if c.Op != trap.Skip {
			n++
		}
	}
	if len(changes) == 0 {
		return
	}
	if n == 0 {
		printChanges(l.Rel, changes)
		fmt.Println("Nothing there can be committed.")
		return
	}

	commit := false
	switch policy {
	case models.WtFlag:
		if !commitFlag {
			fmt.Printf("Discarded %s to %s (run with --commit to keep them).\n", plural(n, "trapped change"), l.Rel)
			return
		}
		printChanges(l.Rel, changes)
		commit = true
	case models.WtAsk:
		if !commitFlag && !tty {
			fmt.Printf("Discarded %s to %s (no terminal to ask on).\n", plural(n, "trapped change"), l.Rel)
			return
		}
		printChanges(l.Rel, changes)
		if commitFlag {
			commit = true
		} else if commit, err = confirm(fmt.Sprintf("Commit these changes to %s?", l.Rel)); err != nil {
			commit = false
		}
	default:
		fmt.Printf("Discarded %s to %s.\n", plural(n, "trapped change"), l.Rel)
		return
	}
	if !commit {
		fmt.Printf("Discarded the changes to %s.\n", l.Rel)
		return
	}

	errs := trap.Apply(l, changes)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
	}
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "Committed the changes to %s, except %s listed above.\n", l.Rel, plural(len(errs), "entry"))
		return
	}
	fmt.Printf("Committed %s to %s.\n", plural(n, "change"), l.Rel)
}

func printChanges(rel string, changes []trap.Change) {
	fmt.Printf("\nWrite-trapped changes in %s (%d):\n", rel, len(changes))
	for i, c := range changes {
		if i == maxListedChanges {
			fmt.Printf("  … and %d more\n", len(changes)-i)
			break
		}
		fmt.Printf("  %s\n", c)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// settleOrphans settles the layers of earlier runs whose dp died before it
// could, applying the current policy and each run's own --commit.
func settleOrphans(policy models.WtCommit, tty bool) error {
	base, err := overlaysDir()
	if err != nil {
		return err
	}
	orphans, err := trap.Orphans(base)
	if err != nil {
		return err
	}
	for _, s := range orphans {
		if len(s.Layers) > 0 {
			fmt.Println("Found write-trapped changes from an earlier run that did not finish.")
		}
		settleSession(s, policy, tty)
	}
	return nil
}
