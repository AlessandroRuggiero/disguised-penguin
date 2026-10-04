package cli

import (
	"reflect"
	"strings"
	"testing"

	"disguised-penguin/internal/models"
)

func TestAliasedCLIs(t *testing.T) {
	clis := []models.CLI{{Name: "zeta", Alias: true}, {Name: "alpha"}, {Name: "beta", Alias: true}}

	cases := map[models.AliasMode][]string{
		models.AliasNone: nil,
		models.AliasSome: {"beta", "zeta"},
		models.AliasAll:  {"alpha", "beta", "zeta"},
	}
	for mode, want := range cases {
		if got := aliasedCLIs(clis, mode); !reflect.DeepEqual(got, want) {
			t.Errorf("mode %d: got %v, want %v", mode, got, want)
		}
	}
}

func TestAliasScript(t *testing.T) {
	names := []string{"opencode", "my.tool-2"}
	cases := map[string][]string{
		"bash":       {"alias opencode='dp opencode'\n", "alias my.tool-2='dp my.tool-2'\n"},
		"zsh":        {"alias opencode='dp opencode'\n"},
		"fish":       {"alias opencode 'dp opencode'\n"},
		"powershell": {"function opencode { dp opencode @args }\n"},
	}
	for shell, wants := range cases {
		script, skipped := aliasScript(shell, names)
		if len(skipped) != 0 {
			t.Errorf("%s: unexpected skipped %v", shell, skipped)
		}
		for _, want := range wants {
			if !strings.Contains(script, want) {
				t.Errorf("%s: script missing %q:\n%s", shell, want, script)
			}
		}
	}
}

func TestAliasScriptSkipsUnsafeNames(t *testing.T) {
	unsafe := []string{"x'; rm -rf ~; '", "a b", "$(id)", "-flag", "", "dp"}
	script, skipped := aliasScript("bash", append([]string{"ok"}, unsafe...))
	if !reflect.DeepEqual(skipped, unsafe) {
		t.Errorf("skipped: got %q, want %q", skipped, unsafe)
	}
	if strings.Count(script, "alias ") != 1 || !strings.Contains(script, "alias ok='dp ok'") {
		t.Errorf("unexpected script:\n%s", script)
	}
}

func TestAliasScriptEmpty(t *testing.T) {
	if script, _ := aliasScript("bash", nil); script != "" {
		t.Errorf("expected empty script, got %q", script)
	}
}
