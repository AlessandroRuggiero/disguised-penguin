package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/db"
	"disguised-penguin/internal/remote"

	"github.com/spf13/cobra"
)

var store *db.Store

var Version = "dev"

var containerRuntimeFlag string

func SetupBindings(dbStore *db.Store) {
	store = dbStore
}

func getInstallSuggestions(cliName string) ([]string, error) {
	availableRemotes, err := store.ListRegistries()
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}
	results, err := remote.FuzzySearchRemotePackages(availableRemotes, cliName)
	if err != nil {
		return nil, fmt.Errorf("failed to search remote packages: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no matching CLI found")
	}
	var names []string
	for name, _ := range results {
		names = append(names, name)
	}
	return names, nil
}

var rootCmd = &cobra.Command{
	Use:                "dp  [cli_name] [args...]",
	Short:              "Run CLI applications in a containerized environment",
	Long:               ``,
	Version:            Version,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		clis, err := store.ListCLIs()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var names []string
		for _, cli := range clis {
			names = append(names, cli.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var cliArgs []string = args
		workspace := "default"
		runtimeRequested := containerRuntimeFlag

		for len(cliArgs) > 0 {
			if cliArgs[0] == "-w" || cliArgs[0] == "--workspace" {
				if len(cliArgs) > 1 {
					workspace = cliArgs[1]
					cliArgs = cliArgs[2:]
				} else {
					return fmt.Errorf("flag needs an argument: '%s'", cliArgs[0])
				}
			} else if cliArgs[0] == "--runtime" {
				if len(cliArgs) > 1 {
					runtimeRequested = cliArgs[1]
					cliArgs = cliArgs[2:]
				} else {
					return fmt.Errorf("flag needs an argument: '%s'", cliArgs[0])
				}
			} else if strings.HasPrefix(cliArgs[0], "--runtime=") {
				runtimeRequested = strings.TrimPrefix(cliArgs[0], "--runtime=")
				cliArgs = cliArgs[1:]
			} else {
				break
			}
		}

		if len(cliArgs) == 0 {
			return fmt.Errorf("requires at least 1 arg, only received 0")
		}

		cliName := cliArgs[0]
		cli, err := store.GetCliByName(cliName)
		if err != nil {
			suggestions, err := getInstallSuggestions(cliName)
			if err != nil {
				log.Printf("failed to get install suggestions: %v", err) // NEVER RETURN AN ERROR THAT IS NOT THE ORIGINAL
				suggestions = []string{}
			}
			suggestion_text := ""
			if len(suggestions) > 0 {
				suggestion_text = fmt.Sprintf("\nDid you mean one of these?\n  %s", strings.Join(suggestions, "\n  "))
			}
			return fmt.Errorf("CLI %s is not installed%s\n", cliName, suggestion_text)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		runtime, err := container.ResolveRuntime(runtimeRequested)
		if err != nil {
			return err
		}

		runtimeArgs := []string{"run", "--rm", "-it", "-v", fmt.Sprintf("%s:/workspace", cwd), "-w", "/workspace"}

		// On Windows, os/user returns SID strings (not numeric IDs), and bind
		// mounts of Windows paths have no real POSIX ownership to match anyway,
		// so PUID/PGID would be meaningless (or break entrypoints expecting a number).
		if goruntime.GOOS != "windows" {
			if currentUser, err := user.Current(); err == nil {
				runtimeArgs = append(runtimeArgs, "-e", fmt.Sprintf("PUID=%s", currentUser.Uid))
				runtimeArgs = append(runtimeArgs, "-e", fmt.Sprintf("PGID=%s", currentUser.Gid))
			} else {
				fmt.Printf("Warning: Could not get current user info: %v. Container may run as root.\n", err)
			}

			// Rootless podman remaps container UIDs through /etc/subuid, so the
			// PUID/PGID dance above would drop privileges onto a subordinate UID
			// that doesn't own the bind-mounted host files. --userns=keep-id maps
			// the invoking user 1:1 into the container so the dropped-to user lines
			// up with the host owner, and --user=0 keeps the entrypoint starting as
			// root so it can still run usermod/chown before dropping privileges.
			// Docker doesn't understand keep-id, and rootful podman rejects it, so
			// this is gated to rootless podman only.
			if runtime == container.RuntimePodman && os.Geteuid() != 0 {
				runtimeArgs = append(runtimeArgs, "--userns=keep-id", "--user=0")
			}
		}

		appDataDir, err := db.GetDBPath()
		if err != nil {
			return fmt.Errorf("failed to get app data dir: %w", err)
		}
		volumesDir := filepath.Join(filepath.Dir(appDataDir), "workspaces", workspace, "volumes", cli.Name)

		for volumeName, containerPath := range cli.ConfigMounts {
			hostVolumePath := filepath.Join(volumesDir, volumeName)
			if err := os.MkdirAll(hostVolumePath, 0755); err != nil {
				return fmt.Errorf("failed to create host volume directory: %w", err)
			}
			runtimeArgs = append(runtimeArgs, "-v", fmt.Sprintf("%s:%s", hostVolumePath, containerPath))
		}

		for hostPort, containerPort := range cli.PortMappings {
			runtimeArgs = append(runtimeArgs, "-p", fmt.Sprintf("%s:%s", hostPort, containerPort))
		}

		runtimeArgs = append(runtimeArgs, cli.ContainerName)
		runtimeArgs = append(runtimeArgs, cliArgs[1:]...)
		runtimeCmd := exec.Command(string(runtime), runtimeArgs...)
		runtimeCmd.Stdin = os.Stdin
		runtimeCmd.Stdout = os.Stdout
		runtimeCmd.Stderr = os.Stderr

		if err := runtimeCmd.Run(); err != nil {
			return fmt.Errorf("failed to run container with %s: %w", runtime, err)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&containerRuntimeFlag, "runtime", string(container.RuntimeAuto), "Container runtime to use: auto, docker, podman (also supports env DP_CONTAINER_RUNTIME)")

	rootCmd.AddCommand(installCompletionsCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(selfUpdateCmd)

	rootCmd.AddCommand(internalCmd)
	internalCmd.AddCommand(eraseDBCmd)

	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryRemoveCmd)
	registryCmd.AddCommand(registryVisitCmd)

	rootCmd.AddCommand(workspaceCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
}
