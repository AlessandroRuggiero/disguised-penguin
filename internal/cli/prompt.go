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
	label := runtimeLabel(runtime)
	fmt.Printf("\nWarning: '%s' requests extra options for %s:\n\n", name, label)
	for _, arg := range args {
		fmt.Printf("  %s\n", arg)
		fmt.Printf("      %s\n", arg.Description)
	}
	fmt.Printf("\nThese are passed to %s every time you run '%s' and may weaken\nthe container's isolation from your machine.\n\n", label, name)
}

// Explains what dp hands a graphical app on every run (see container.AppRunArgs).
func printAppAccess(name string) {
	fmt.Printf("\nWarning: '%s' is a graphical app. Every time you run it, dp gives it:\n\n", name)
	fmt.Println("  - your Wayland display socket, so it can open windows (and see clipboard and input sent to them)")
	fmt.Println("  - your GPU: /dev/dri, plus NVIDIA GPUs with CUDA when the NVIDIA Container Toolkit is installed")
	fmt.Println("  - 1GB of shared memory")
	fmt.Println("  - your time zone")
	fmt.Println("  - on SELinux hosts, no SELinux confinement, which the display socket needs")
	fmt.Println()
}

func runtimeLabel(runtime container.Runtime) string {
	if runtime == "" {
		return "the container runtime"
	}
	return string(runtime)
}

// Asks the user to accept the access a package needs beyond the default
// sandbox: being a graphical app, extra run args, or both. Nothing requested
// means nothing to ask.
func confirmPackageAccess(runtime container.Runtime, name, question string, app bool, args []models.ExtraRunArg, assumeYes bool) (bool, error) {
	if !app && len(args) == 0 {
		return true, nil
	}
	if app {
		printAppAccess(name)
	}
	if len(args) > 0 {
		printExtraRunArgs(runtime, name, args)
	}
	if assumeYes {
		fmt.Println("Accepted via --yes.")
		return true, nil
	}
	accepted, err := confirm(question)
	if errors.Is(err, errNoInput) {
		return false, fmt.Errorf("'%s' requests extra access (listed above) and nothing answered the prompt.\nRe-run with --yes to accept it", name)
	}
	return accepted, err
}
