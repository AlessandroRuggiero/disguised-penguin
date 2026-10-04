package container

import (
	"slices"
	"testing"
)

func TestInteractiveFlags(t *testing.T) {
	if got := InteractiveFlags(true); !slices.Equal(got, []string{"-i", "-t"}) {
		t.Errorf("with tty: got %v, want [-i -t]", got)
	}
	if got := InteractiveFlags(false); !slices.Equal(got, []string{"-i"}) {
		t.Errorf("without tty: got %v, want [-i]", got)
	}
}
