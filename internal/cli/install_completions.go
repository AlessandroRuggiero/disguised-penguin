package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var installCompletionsCmd = &cobra.Command{
	Use:   "install-completions",
	Short: "Install shell completions for the current user",
	RunE: func(cmd *cobra.Command, args []string) error {
		shell := os.Getenv("SHELL")
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not get home directory: %w", err)
		}

		if strings.Contains(shell, "zsh") {
			rcPath := filepath.Join(home, ".zshrc")
			return appendIfNotPresent(rcPath, "source <(dp completion zsh)")
		} else if strings.Contains(shell, "bash") {
			rcPath := filepath.Join(home, ".bashrc")
			return appendIfNotPresent(rcPath, "source <(dp completion bash)")
		}

		return fmt.Errorf("unsupported or unknown shell: %s. Supported shells are bash and zsh.", shell)
	},
}

func appendIfNotPresent(path, line string) error {
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		// If it does not exist, we'll just create it below
	} else {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), line) {
				fmt.Printf("Completions already installed in %s\n", path)
				return nil
			}
		}
	}

	// Append the line
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", path, err)
	}
	defer f.Close()

	content := fmt.Sprintf("\n# disguised-penguin completions\n%s\n", line)
	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}

	fmt.Printf("Completions installed in %s. Please restart your shell or run 'source %s' to apply.\n", path, path)
	return nil
}
