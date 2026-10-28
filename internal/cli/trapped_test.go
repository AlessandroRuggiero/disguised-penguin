//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"disguised-penguin/internal/models"
	"disguised-penguin/internal/trap"
)

// trappedLayer is a layer over a fresh host dir in which the tool added new.txt.
func trappedLayer(t *testing.T) trap.Layer {
	t.Helper()
	l := trap.Layer{Host: t.TempDir(), Rel: "src", Dir: t.TempDir()}
	if err := os.MkdirAll(l.Upper(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.Upper(), "new.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	return l
}

// The prompt is never reached in these cases, so no stdin is needed.
func TestSettleLayerPolicies(t *testing.T) {
	cases := []struct {
		name       string
		policy     models.WtCommit
		commitFlag bool
		tty        bool
		committed  bool
	}{
		{"discard ignores the terminal", models.WtDiscard, false, true, false},
		{"flag without --commit", models.WtFlag, false, true, false},
		{"flag with --commit", models.WtFlag, true, false, true},
		{"ask with no terminal", models.WtAsk, false, false, false},
		{"ask with --commit skips the prompt", models.WtAsk, true, false, true},
	}
	for _, tc := range cases {
		l := trappedLayer(t)
		settleLayer(l, tc.policy, tc.commitFlag, tc.tty)
		_, err := os.Stat(filepath.Join(l.Host, "new.txt"))
		if committed := err == nil; committed != tc.committed {
			t.Errorf("%s: committed = %v, want %v", tc.name, committed, tc.committed)
		}
	}
}

func TestPlural(t *testing.T) {
	for _, tc := range []struct {
		n    int
		word string
		want string
	}{
		{1, "change", "1 change"},
		{3, "trapped change", "3 trapped changes"},
		{2, "entry", "2 entries"},
	} {
		if got := plural(tc.n, tc.word); got != tc.want {
			t.Errorf("plural(%d, %q) = %q, want %q", tc.n, tc.word, got, tc.want)
		}
	}
}
