package cli

import (
	"bufio"
	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Shared, so a second prompt keeps what the first one buffered.
var stdinReader = bufio.NewReader(os.Stdin)

// Stdin was empty, so nobody was there to answer.
var errNoInput = errors.New("no input on stdin")

// Asks a yes/no question. Anything but an explicit yes is a no.
func confirm(question string) (bool, error) {
	fmt.Printf("%s (y/N): ", question)
	line, err := stdinReader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read response: %w", err)
	}
	if err == io.EOF && strings.TrimSpace(line) == "" {
		return false, errNoInput
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Lists the args a package wants passed to the runtime, with their descriptions.
func printExtraRunArgs(runtime container.Runtime, name string, args []models.ExtraRunArg) {
	label := string(runtime)
	if label == "" {
		label = "the container runtime"
	}
	fmt.Printf("\nWarning: '%s' requests extra options for %s:\n\n", name, label)
	for _, arg := range args {
		fmt.Printf("  %s\n", arg)
		fmt.Printf("      %s\n", arg.Description)
	}
	fmt.Printf("\nThese are passed to %s every time you run '%s' and may weaken\nthe container's isolation from your machine.\n\n", label, name)
}

// Asks the user to accept a package's extra run args. No args means nothing to ask.
func confirmExtraRunArgs(runtime container.Runtime, name, question string, args []models.ExtraRunArg, assumeYes bool) (bool, error) {
	if len(args) == 0 {
		return true, nil
	}
	printExtraRunArgs(runtime, name, args)
	if assumeYes {
		fmt.Println("Accepted via --yes.")
		return true, nil
	}
	accepted, err := confirm(question)
	if errors.Is(err, errNoInput) {
		return false, fmt.Errorf("'%s' requests extra run args (listed above) and nothing answered the prompt.\nRe-run with --yes to accept them", name)
	}
	return accepted, err
}
