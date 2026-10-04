package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"

	"github.com/spf13/cobra"
)

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func rebuildVariants(runtime container.Runtime, cliNames []string) error {
	var targets []models.TrackedVariant
	for _, name := range cliNames {
		variants, err := store.ListVariants(name)
		if err != nil {
			return err
		}
		targets = append(targets, variants...)
	}
	if len(targets) == 0 {
		fmt.Println("No tracked variants to rebuild.")
		return nil
	}

	var failed []string
	for i, tv := range targets {
		fmt.Printf("[%d/%d] Rebuilding variant of '%s' in %s...\n", i+1, len(targets), tv.CLIName, tv.ProjectDir)

		// Gone or undeclared: nothing to rebuild, so skip rather than fail.
		if !dirExists(tv.ProjectDir) {
			fmt.Println("Skipped: the project folder no longer exists (remove it with 'dp variants prune').")
			continue
		}
		declared, err := loadVariants(tv.ProjectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
			failed = append(failed, tv.CLIName+" in "+tv.ProjectDir)
			continue
		}
		variant, ok := declared[tv.CLIName]
		if !ok {
			fmt.Printf("Skipped: %s no longer declares it (remove it with 'dp variants prune').\n", variantsFile)
			continue
		}

		if err := buildVariant(runtime, variant, tv.ProjectDir); err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
			failed = append(failed, tv.CLIName+" in "+tv.ProjectDir)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to rebuild variants: %s", strings.Join(failed, ", "))
	}
	return nil
}

// parseAge is a Go duration plus days ("30d") and weeks ("2w").
func parseAge(s string) (time.Duration, error) {
	invalid := fmt.Errorf("invalid age %q, use e.g. 30d, 2w or 36h", s)

	var unit time.Duration
	switch {
	case strings.HasSuffix(s, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(s, "w"):
		unit = 7 * 24 * time.Hour
	default:
		d, err := time.ParseDuration(s)
		if err != nil || d < 0 {
			return 0, invalid
		}
		return d, nil
	}

	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return 0, invalid
	}
	return time.Duration(n) * unit, nil
}

func prunable(variants []models.TrackedVariant, now time.Time, maxAge time.Duration, exists func(string) bool) []models.TrackedVariant {
	var selected []models.TrackedVariant
	for _, v := range variants {
		if now.Sub(v.LastActivity()) > maxAge || !exists(v.ProjectDir) {
			selected = append(selected, v)
		}
	}
	return selected
}

func formatAgo(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// The record is only dropped once the image is gone, so a failed rmi
// doesn't leave an image dp has forgotten about.
func removeVariant(runtime container.Runtime, v models.TrackedVariant, imageShared bool) error {
	if !imageShared {
		exists, err := imageExists(runtime, v.Image)
		if err != nil {
			return err
		}
		if exists {
			out, err := exec.Command(string(runtime), "rmi", v.Image).CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to remove image '%s': %s", v.Image, strings.TrimSpace(string(out)))
			}
		}
	}
	return store.RemoveVariant(v.ID)
}

var variantsCmd = &cobra.Command{
	Use:   "variants",
	Short: "List and prune variants across all projects",
	Long: `List and prune variants across all projects.

dp records every variant it builds or runs, so it can find them again from
anywhere. To declare or build a variant for the current project, use
'dp local variant'. To rebuild variants after their CLI updates, use
'dp update --variants'.`,
}

var variantsListCmd = &cobra.Command{
	Use:               "list [cli]",
	Aliases:           []string{"ls"},
	Short:             "List the tracked variants",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeInstalledCLIs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cliName := ""
		if len(args) == 1 {
			cliName = args[0]
		}
		variants, err := store.ListVariants(cliName)
		if err != nil {
			return err
		}
		if len(variants) == 0 {
			fmt.Println("No tracked variants. They are recorded when built with 'dp local variant build' or run.")
			return nil
		}

		now := time.Now()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CLI\tPROJECT\tBUILT\tLAST USED\tSTATUS")
		for _, v := range variants {
			status := "ok"
			if !dirExists(v.ProjectDir) {
				status = "missing folder"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.CLIName, v.ProjectDir, formatAgo(v.BuiltAt, now), formatAgo(v.LastUsedAt, now), status)
		}
		return w.Flush()
	},
}

var (
	pruneOlderThanFlag string
	pruneYesFlag       bool
)

var variantsPruneCmd = &cobra.Command{
	Use:   "prune --older-than <age>",
	Short: "Remove variants not used recently, and those whose project is gone",
	Long: `Remove the variants that have not been run within <age>, plus those whose
project folder no longer exists. A variant that was never run counts from
when it was built. Rebuilding does not count as using it.

Each variant's image and dp's record of it are removed. The project's
.dp/variants.json is left alone, so 'dp local variant build' brings it back.`,
	Example: `  dp variants prune --older-than 30d
  dp variants prune --older-than 2w --yes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		maxAge, err := parseAge(pruneOlderThanFlag)
		if err != nil {
			return err
		}
		variants, err := store.ListVariants("")
		if err != nil {
			return err
		}
		now := time.Now()
		selected := prunable(variants, now, maxAge, dirExists)
		if len(selected) == 0 {
			fmt.Println("Nothing to prune.")
			return nil
		}

		fmt.Println("These variants will be removed:")
		for _, v := range selected {
			reason := "last used " + formatAgo(v.LastActivity(), now)
			if !dirExists(v.ProjectDir) {
				reason = "project folder is gone"
			}
			fmt.Printf("  - %s in %s (%s)\n", v.CLIName, v.ProjectDir, reason)
		}

		if !pruneYesFlag {
			ok, err := confirm("Remove them?")
			if errors.Is(err, errNoInput) {
				return fmt.Errorf("nothing answered the prompt; re-run with --yes to remove them")
			}
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Nothing removed.")
				return nil
			}
		}

		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			return err
		}

		// CLIs sharing an image share the variant tag; keep it if still used.
		pruning := map[int]bool{}
		for _, v := range selected {
			pruning[v.ID] = true
		}
		kept := map[string]bool{}
		for _, v := range variants {
			if !pruning[v.ID] {
				kept[v.Image] = true
			}
		}

		var failed []string
		for _, v := range selected {
			if err := removeVariant(runtime, v, kept[v.Image]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove variant of '%s' in %s: %v\n", v.CLIName, v.ProjectDir, err)
				failed = append(failed, v.CLIName+" in "+v.ProjectDir)
				continue
			}
			fmt.Printf("Removed variant of '%s' in %s\n", v.CLIName, v.ProjectDir)
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed to prune: %s", strings.Join(failed, ", "))
		}
		return nil
	},
}
