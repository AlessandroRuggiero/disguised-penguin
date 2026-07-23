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

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Variants come from the directory dp runs in.
		variants, err := loadVariants(cwd)
		if err != nil {
			return err
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

		// Check whether there is a local variant
		_, isVariant := variants[cliName]

		dbWorkspace, exists, err := store.GetWorkspaceByName(workspaceName)

		if err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found (create it with 'dp workspace add %s')", workspaceName, workspaceName)
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

		for volumeName, mount := range cli.ConfigMounts {
			hostVolumePath := ws.VolumeDir(cli.Name, volumeName)
			if mount.IsFile() {
				// Bind a file: ensure the parent dir and an empty file exist, so
				// the runtime binds a file rather than auto-creating a directory.
				if err := os.MkdirAll(filepath.Dir(hostVolumePath), 0755); err != nil {
					return fmt.Errorf("failed to create host volume directory: %w", err)
				}
				if _, err := os.Stat(hostVolumePath); os.IsNotExist(err) {
					f, err := os.OpenFile(hostVolumePath, os.O_CREATE, 0644)
					if err != nil {
						return fmt.Errorf("failed to create host volume file: %w", err)
					}
					f.Close()
				}
			} else if err := os.MkdirAll(hostVolumePath, 0755); err != nil {
				return fmt.Errorf("failed to create host volume directory: %w", err)
			}
			runtimeArgs = append(runtimeArgs, "-v", fmt.Sprintf("%s:%s%s", hostVolumePath, mount.Path, mountSuffix))
		}

		// Mount protections (--mp PATH:MODE) overlay a nested mount on top of the
		// /workspace bind so a subpath can be made read-only, writable, or hidden
		// (shadowed by an empty mount). The runtime mounts by destination depth, so
		// these always shadow the broader /workspace mount regardless of arg order.
		dbPath, err := db.GetDBPath()
		if err != nil {
			return fmt.Errorf("failed to resolve data directory: %w", err)
		}
		hides := container.NewHidePlaceholders(filepath.Join(filepath.Dir(dbPath), "placeholders"))

		// Add workspace-level mount protections from the database, so they apply to all CLIs in the workspace.
		workspaceProtections, err := store.GetMountProtections(dbWorkspace.ID)
		if err != nil {
			return fmt.Errorf("failed to get mount protections: %w", err)
		}
		workspaceProtSpecs := make([]string, len(workspaceProtections))
		for i, prot := range workspaceProtections {
			workspaceProtSpecs[i] = prot.String()
		}
		// Workspace-level mount protections are applied first, so CLI-level mount protections can override them if needed.
		mpSpecs = append(mpSpecs, workspaceProtSpecs...)
		for _, spec := range mpSpecs {
			prot, err := container.ParseProtection(spec)
			if err != nil {
				return err
			}
			hostPath := filepath.Join(cwd, prot.Rel)
			info, err := os.Stat(hostPath)
			missing := os.IsNotExist(err)
			if err != nil && !missing {
				return fmt.Errorf("mount protection %q: cannot access %s: %w", spec, prot.Rel, err)
			}
			// Every mode binds onto an existing mountpoint, so the path must
			// exist on the host. Hiding a not-yet-created path would force the
			// runtime to create a nested mountpoint, which Docker Desktop's
			// virtiofs rejects ("outside of rootfs").
			if missing {
				continue
			}
			dest := path.Join("/workspace", filepath.ToSlash(prot.Rel))

			switch prot.Mode {
			case "ro", "rw":
				runtimeArgs = append(runtimeArgs, "-v", hostPath+":"+dest+container.MountOpts(prot.Mode, selinux))
			case "h":
				// Hide the host path by binding an empty placeholder over it.
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
		if isVariant {
			// If the CLI is a variant, check if it has already been built
			built, err := isVariantBuilt(runtime, cli.Image, cwd)
			if err != nil {
				return fmt.Errorf("failed to check if variant is built: %w", err)
			}
			if !built {
				return fmt.Errorf("variant of '%s' has not been built yet. Please run 'dp local variant build %s' first", cliName, cliName)
			}

			// If the CLI is a variant, use its variant image.
			variantImageRef := variantImage(cli.Image, cwd)
			runtimeArgs = append(runtimeArgs, variantImageRef)
		} else {
			// If the CLI is not a variant, use its image directly.
			runtimeArgs = append(runtimeArgs, cli.Image)
		}

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

	rootCmd.AddCommand(localCmd)
	localCmd.AddCommand(localVariantCmd)
	localVariantCmd.AddCommand(localVariantBuildCmd)
	localVariantCmd.AddCommand(localVariantExtendCmd)

	rootCmd.AddCommand(workspaceCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceCleanCmd)
	workspaceCmd.AddCommand(workspaceListCmd)

	workspaceCmd.AddCommand(workspaceProtectionCmd)
	workspaceProtectionCmd.AddCommand(workspaceProtectionAddCmd)
	workspaceProtectionCmd.AddCommand(workspaceProtectionRemoveCmd)
	workspaceProtectionCmd.AddCommand(workspaceProtectionListCmd)
}
