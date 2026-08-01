package cli

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"
)

// withStdin swaps the shared prompt reader for a canned response.
func withStdin(t *testing.T, input string) {
	t.Helper()
	prev := stdinReader
	stdinReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { stdinReader = prev })
}

func TestConfirm(t *testing.T) {
	cases := map[string]bool{
		"y\n":     true,
		"Y\n":     true,
		"yes\n":   true,
		" y \n":   true,
		"YES\n":   true,
		"n\n":     false,
		"\n":      false,
		"y":       true, // answered, then stdin closed without a newline
		"maybe\n": false,
	}
	for input, want := range cases {
		withStdin(t, input)
		got, err := confirm("proceed?")
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", input, err)
		}
		if got != want {
			t.Errorf("%q: got %v, want %v", input, got, want)
		}
	}
}

// Empty stdin is not a "no", it means nobody was there to be asked.
func TestConfirmNoInput(t *testing.T) {
	withStdin(t, "")
	got, err := confirm("proceed?")
	if !errors.Is(err, errNoInput) {
		t.Fatalf("expected errNoInput, got %v", err)
	}
	if got {
		t.Error("expected a refusal")
	}
}

func TestConfirmExtraRunArgsNoArgs(t *testing.T) {
	// No args means nothing to warn about, so it must not touch stdin.
	ok, err := confirmExtraRunArgs(container.RuntimeDocker, "tool", "continue?", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected a package without extra run args to be accepted")
	}
}

func TestConfirmExtraRunArgsAssumeYes(t *testing.T) {
	args := []models.ExtraRunArg{{Args: []string{"--privileged"}, Description: "why"}}
	ok, err := confirmExtraRunArgs(container.RuntimeDocker, "tool", "continue?", args, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected --yes to accept the args")
	}
}

// Nothing on stdin must be an error pointing at --yes, not a silent accept.
func TestConfirmExtraRunArgsNoInput(t *testing.T) {
	withStdin(t, "")
	args := []models.ExtraRunArg{{Args: []string{"--privileged"}, Description: "why"}}
	ok, err := confirmExtraRunArgs(container.RuntimeDocker, "tool", "continue?", args, false)
	if err == nil {
		t.Fatal("expected an error when nothing answers the prompt")
	}
	if ok {
		t.Error("expected the args to be refused")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should point at --yes, got: %v", err)
	}
}

func TestExtraRunArgsEqual(t *testing.T) {
	base := []models.ExtraRunArg{
		{Args: []string{"--cap-add", "SYS_PTRACE"}, Description: "attach"},
		{Args: []string{"--network=host"}, Description: "local services"},
	}

	cases := map[string]struct {
		other []models.ExtraRunArg
		want  bool
	}{
		"identical":      {base, true},
		"other is empty": {nil, false},
		"different flag": {
			[]models.ExtraRunArg{
				{Args: []string{"--cap-add", "SYS_ADMIN"}, Description: "attach"},
				{Args: []string{"--network=host"}, Description: "local services"},
			},
			false,
		},
		// Same flags, new justification: still worth showing the user again.
		"different description": {
			[]models.ExtraRunArg{
				{Args: []string{"--cap-add", "SYS_PTRACE"}, Description: "something else"},
				{Args: []string{"--network=host"}, Description: "local services"},
			},
			false,
		},
		"reordered": {
			[]models.ExtraRunArg{base[1], base[0]},
			false,
		},
		"extra entry": {
			append(append([]models.ExtraRunArg{}, base...), models.ExtraRunArg{Args: []string{"--privileged"}, Description: "no"}),
			false,
		},
	}
	for name, tc := range cases {
		if got := extraRunArgsEqual(base, tc.other); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}

	if !extraRunArgsEqual(nil, nil) {
		t.Error("two empty lists should compare equal")
	}
}
