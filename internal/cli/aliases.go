package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"disguised-penguin/internal/models"

	"github.com/spf13/cobra"
)

// Aliases let installed CLIs be run without the "dp" prefix. They are appended
// to the output of "dp completion <shell>", so the line "dp install-completions"
// already put in the user's rc file loads them too, and every new shell picks
// up installs, removals and alias_mode changes.

// CLI names come from remote registries and end up in a script the shell
// sources, so only plain names are ever turned into aliases.
var aliasNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func aliasNameValid(name string) bool {
	return aliasNameRe.MatchString(name) && name != "dp"
}

// aliasedCLIs returns the sorted names of the CLIs that get an alias under mode.
func aliasedCLIs(clis []models.CLI, mode models.AliasMode) []string {
	var names []string
	for _, c := range clis {
		if mode == models.AliasAll || (mode == models.AliasSome && c.Alias) {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return names
}

// aliasScript renders the alias definitions for shell. Unsafe names are
// skipped and returned so the caller can warn about them.
func aliasScript(shell string, names []string) (script string, skipped []string) {
	var b strings.Builder
	for _, name := range names {
		if !aliasNameValid(name) {
			skipped = append(skipped, name)
			continue
		}
		switch shell {
		case "bash", "zsh":
			fmt.Fprintf(&b, "alias %s='dp %s'\n", name, name)
		case "fish":
			fmt.Fprintf(&b, "alias %s 'dp %s'\n", name, name)
		case "powershell":
			// PowerShell aliases can't carry arguments, so use a function.
			fmt.Fprintf(&b, "function %s { dp %s @args }\n", name, name)
		}
	}
	if b.Len() == 0 {
		return "", skipped
	}
	return "\n# disguised-penguin aliases (dp settings set alias_mode ...)\n" + b.String(), skipped
}

// writeAliases appends the alias block for shell. It never fails: the output
// is sourced at shell startup, so a problem only costs the aliases.
func writeAliases(out io.Writer, shell string) {
	mode, err := store.GetSetting(models.SettingAliasMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dp: skipping aliases: %v\n", err)
		return
	}
	if models.AliasMode(mode) == models.AliasNone {
		return
	}
	clis, err := store.ListCLIs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dp: skipping aliases: %v\n", err)
		return
	}
	script, skipped := aliasScript(shell, aliasedCLIs(clis, models.AliasMode(mode)))
	for _, name := range skipped {
		fmt.Fprintf(os.Stderr, "dp: not aliasing '%s': name is not safe to use as a shell alias\n", name)
	}
	fmt.Fprint(out, script)
}

// hookCompletionCmd makes cobra create its default "completion" command now
// (instead of lazily inside Execute) and wraps each shell's generator so the
// alias block follows the completion script.
func hookCompletionCmd(args []string) {
	rootCmd.InitDefaultCompletionCmd(args...)
	for _, c := range rootCmd.Commands() {
		if c.Name() != "completion" {
			continue
		}
		for _, sub := range c.Commands() {
			gen, shell := sub.RunE, sub.Name()
			if gen == nil {
				continue
			}
			sub.RunE = func(cmd *cobra.Command, args []string) error {
				if err := gen(cmd, args); err != nil {
					return err
				}
				writeAliases(cmd.OutOrStdout(), shell)
				return nil
			}
		}
	}
}

func completeInstalledCLIs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	clis, err := store.ListCLIs()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, c := range clis {
		names = append(names, c.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func setCLIAlias(name string, enabled bool) error {
	n, err := store.SetCLIAlias(name, enabled)
	if err != nil {
		return fmt.Errorf("failed to update CLI: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("CLI '%s' is not installed", name)
	}
	if enabled {
		fmt.Printf("Enabled alias for '%s'\n", name)
		if !aliasNameValid(name) {
			fmt.Printf("Warning: '%s' is not a safe shell alias name, so no alias will be generated for it.\n", name)
		} else if path, err := exec.LookPath(name); err == nil {
			fmt.Printf("Warning: the alias will shadow '%s' in your shell.\n", path)
		}
	} else {
		fmt.Printf("Disabled alias for '%s'\n", name)
	}

	mode, err := store.GetSetting(models.SettingAliasMode)
	if err != nil {
		return err
	}
	if models.AliasMode(mode) != models.AliasSome {
		def, _ := models.LookupSetting(string(models.SettingAliasMode))
		fmt.Printf("Note: alias_mode is '%s', so per-CLI aliases have no effect until you run 'dp settings set alias_mode some'.\n", def.ValueName(mode))
	} else {
		fmt.Println("Open a new shell to apply.")
	}
	return nil
}

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Choose which CLIs can be run without the 'dp' prefix",
	Long: `Choose which CLIs can be run without the 'dp' prefix.

Which CLIs get an alias is controlled by the alias_mode setting:
  none  no aliases (default)
  some  only CLIs enabled with 'dp alias add'
  all   every installed CLI

Aliases are loaded together with shell completions ('dp install-completions').`,
}

var aliasAddCmd = &cobra.Command{
	Use:               "add [cli]",
	Short:             "Enable the alias for a CLI (used when alias_mode is 'some')",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeInstalledCLIs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setCLIAlias(args[0], true)
	},
}

var aliasRemoveCmd = &cobra.Command{
	Use:               "remove [cli]",
	Aliases:           []string{"rm"},
	Short:             "Disable the alias for a CLI (used when alias_mode is 'some')",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeInstalledCLIs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setCLIAlias(args[0], false)
	},
}

var aliasListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List the CLIs that currently get an alias",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := store.GetSetting(models.SettingAliasMode)
		if err != nil {
			return err
		}
		clis, err := store.ListCLIs()
		if err != nil {
			return err
		}
		def, _ := models.LookupSetting(string(models.SettingAliasMode))
		fmt.Printf("alias_mode: %s\n", def.ValueName(mode))

		names := aliasedCLIs(clis, models.AliasMode(mode))
		if len(names) == 0 {
			fmt.Println("No CLIs are aliased.")
			return nil
		}
		fmt.Println("Aliased CLIs:")
		for _, name := range names {
			note := ""
			if !aliasNameValid(name) {
				note = " (skipped: not a safe alias name)"
			} else if path, err := exec.LookPath(name); err == nil {
				note = fmt.Sprintf(" (shadows %s)", path)
			}
			fmt.Printf("- %s%s\n", name, note)
		}
		return nil
	},
}
