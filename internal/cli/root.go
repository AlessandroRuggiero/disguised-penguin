package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/db"
	"disguised-penguin/internal/remote"
	"disguised-penguin/internal/workspace"

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
		workspaceName := "default"
		runtimeRequested := containerRuntimeFlag
		var mpSpecs []string

		for len(cliArgs) > 0 {
			if cliArgs[0] == "-w" || cliArgs[0] == "--workspace" {
				if len(cliArgs) > 1 {
					workspaceName = cliArgs[1]
					cliArgs = cliArgs[2:]
				} else {
					return fmt.Errorf("flag needs an argument: '%s'", cliArgs[0])
				}
			} else if cliArgs[0] == "--mp" {
				if len(cliArgs) > 1 {
					mpSpecs = append(mpSpecs, cliArgs[1])
					cliArgs = cliArgs[2:]
				} else {
					return fmt.Errorf("flag needs an argument: '%s'", cliArgs[0])
				}
			} else if strings.HasPrefix(cliArgs[0], "--mp=") {
				mpSpecs = append(mpSpecs, strings.TrimPrefix(cliArgs[0], "--mp="))
				cliArgs = cliArgs[1:]
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
			} else if cliArgs[0] == "-v" || cliArgs[0] == "--version" {
				// DisableFlagParsing means cobra won't handle these itself; do it
				// here while we're still in the command position (before a cli name
				// is resolved), so "dp <tool> --version" still passes through to the tool.
				fmt.Printf("dp version %s\n", Version)
				return nil
			} else if cliArgs[0] == "-h" || cliArgs[0] == "--help" {
				return cmd.Help()
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

		if _, exists, err := store.GetWorkspaceByName(workspaceName); err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found (create it with 'dp workspace add %s')", workspaceName, workspaceName)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		runtime, err := container.ResolveRuntime(runtimeRequested)
		if err != nil {
			return err
		}

		// On SELinux-enforcing hosts (Fedora/RHEL and family, the common podman
		// platforms) the container's confined type cannot access host files that
		// carry their original labels, so bind mounts get "Permission denied" even
		// when the UID/GID is correct. The ":z" suffix asks the runtime to relabel
		// the mounted path to a shared container-accessible type. We use ":z"
		// (shared), not ":Z" (private/exclusive), because the workspace is the
		// user's cwd and a private relabel could break other tools' access to it.
		// The suffix is only added when SELinux is actually active, so non-SELinux
		// hosts (Ubuntu/Debian, macOS, Windows) are left untouched.
		selinux := container.SELinuxEnabled()
		mountSuffix := container.MountOpts("", selinux)

		runtimeArgs := []string{"run", "--rm", "-it", "-v", fmt.Sprintf("%s:/workspace%s", cwd, mountSuffix), "-w", "/workspace"}

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

		ws, err := workspace.Get(workspaceName)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace '%s': %w", workspaceName, err)
		}

		for volumeName, containerPath := range cli.ConfigMounts {
			hostVolumePath := ws.VolumeDir(cli.Name, volumeName)
			if err := os.MkdirAll(hostVolumePath, 0755); err != nil {
				return fmt.Errorf("failed to create host volume directory: %w", err)
			}
			runtimeArgs = append(runtimeArgs, "-v", fmt.Sprintf("%s:%s%s", hostVolumePath, containerPath, mountSuffix))
		}

		// Mount protections (--mp PATH:MODE) overlay a nested mount on top of the
		// /workspace bind so a subpath can be made read-only, writable, or hidden
		// (shadowed by an empty mount). The runtime mounts by destination depth, so
		// these always shadow the broader /workspace mount regardless of arg order.
		hides := container.NewHidePlaceholders()
		defer hides.Cleanup()
		for _, spec := range mpSpecs {
			prot, err := container.ParseProtection(spec)
			if err != nil {
				return err
			}
			hostPath := filepath.Join(cwd, prot.Rel)
			info, err := os.Stat(hostPath)
			if err != nil {
				return fmt.Errorf("mount protection %q: cannot access %s: %w", spec, prot.Rel, err)
			}
			dest := path.Join("/workspace", filepath.ToSlash(prot.Rel))

			switch prot.Mode {
			case "ro", "rw":
				runtimeArgs = append(runtimeArgs, "-v", hostPath+":"+dest+container.MountOpts(prot.Mode, selinux))
			case "h":
				// Hide = shadow with an empty, read-only mount so the tool sees
				// nothing (a present-but-empty path; a bind can't truly unlink it).
				empty, err := hides.Get(info.IsDir())
				if err != nil {
					return fmt.Errorf("mount protection %q: %w", spec, err)
				}
				runtimeArgs = append(runtimeArgs, "-v", empty+":"+dest+container.MountOpts("ro", selinux))
			}
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
	workspaceCmd.AddCommand(workspaceCleanCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
}
