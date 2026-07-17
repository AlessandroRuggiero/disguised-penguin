package cli

import (
	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"
	"disguised-penguin/internal/remote"
	"disguised-penguin/internal/workspace"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func printKeyValueSection(title string, m map[string]string) {
	fmt.Printf("%s:\n", title)
	if len(m) == 0 {
		fmt.Println("  (none)")
		return
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Printf("  - %s -> %s\n", k, m[k])
	}
}

var addCmd = &cobra.Command{
	Use:               "add [name] [container_name]",
	Aliases:           []string{"a"},
	Short:             "Add a new CLI configuration to the database",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		containerName := args[1]
		if err := store.AddCLI(name, containerName); err != nil {
			return fmt.Errorf("failed to insert CLI into db: %w", err)
		}
		fmt.Printf("Successfully added CLI '%s' mapped to container '%s'\n", name, containerName)
		return nil
	},
}

var rmCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm", "r"},
	Short:   "Remove a CLI configuration from the database",
	Args:    cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		clis, err := store.ListCLIs()
		if err != nil {
			return fmt.Errorf("failed to list CLIs: %w", err)
		}

		var containerName string
		found := false
		sharedByOthers := false
		for _, c := range clis {
			if c.Name == name {
				containerName = c.ContainerName
				found = true
			}
		}
		if !found {
			fmt.Printf("No CLI found with name '%s'\n", name)
			return nil
		}
		for _, c := range clis {
			if c.Name != name && c.ContainerName == containerName {
				sharedByOthers = true
				break
			}
		}

		if _, err := store.RemoveCLI(name); err != nil {
			return fmt.Errorf("failed to delete CLI from db: %w", err)
		}
		fmt.Printf("Successfully removed CLI '%s'\n", name)

		if sharedByOthers {
			fmt.Printf("Image '%s' is still used by another CLI, leaving it in place.\n", containerName)
			return nil
		}

		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			fmt.Printf("Warning: could not resolve a container runtime to remove image '%s': %v\n", containerName, err)
			return nil
		}
		runtimeCmd := exec.Command(string(runtime), "rmi", containerName)
		runtimeCmd.Stdout = os.Stdout
		runtimeCmd.Stderr = os.Stderr
		if err := runtimeCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to remove image '%s': %v\n", containerName, err)
			return nil
		}
		fmt.Printf("Successfully removed image '%s'\n", containerName)
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:               "install [name]",
	Aliases:           []string{"i"},
	Short:             "Install a CLI configuration by pulling the associated container image",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			return err
		}
		pkgToInstall, exists, err := store.SearchRemotePackageByName(name)
		if err != nil {
			suggestions, err := getInstallSuggestions(name)
			if err != nil {
				log.Printf("failed to get install suggestions: %v", err) // NEVER RETURN AN ERROR THAT IS NOT THE ORIGINAL
				suggestions = []string{}
			}
			suggestionText := ""
			if len(suggestions) > 0 {
				suggestionText = fmt.Sprintf("\nDid you mean one of these?\n  %s", strings.Join(suggestions, "\n  "))
			}
			return fmt.Errorf("package '%s' not found in any remote registry. %s", name, suggestionText)
		}
		if !exists {
			return fmt.Errorf("package '%s' not found in any remote registry", name)
		}

		// make sure it is not already installed
		if _, err := store.GetCliByName(name); err == nil {
			return fmt.Errorf("CLI '%s' is already installed", name)
		}

		fmt.Printf("Pulling image '%s' for CLI '%s' using %s...\n", pkgToInstall.Container, name, runtime)
		runtimeCmd := exec.Command(string(runtime), "pull", pkgToInstall.Container)
		runtimeCmd.Stdout = os.Stdout
		runtimeCmd.Stderr = os.Stderr

		if err := runtimeCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull image with %s: %w", runtime, err)
		}
		fmt.Printf("Successfully pulled image '%s'\n", pkgToInstall.Container)

		printKeyValueSection("Config mounts", pkgToInstall.ConfigMounts)
		printKeyValueSection("Port mappings", pkgToInstall.PortMappings)

		if err := store.InstallCLI(name, pkgToInstall); err != nil {
			return fmt.Errorf("failed to insert CLI into db: %w", err)
		}

		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available CLIs in the database",
	RunE: func(cmd *cobra.Command, args []string) error {
		clis, err := store.ListCLIs()
		if err != nil {
			return err
		}
		fmt.Println("Available CLIs:")
		for _, c := range clis {
			fmt.Printf("- %s (container: %s)\n", c.Name, c.ContainerName)
		}
		return nil
	},
}

var internalCmd = &cobra.Command{
	Use:   "internal",
	Short: "Internal maintenance commands",
}

var eraseDBCmd = &cobra.Command{
	Use:   "erase-db",
	Short: "Erase the entire CLI database (use with caution)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print("Are you sure you want to erase the entire CLI database? This action cannot be undone. (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborting database erase.")
			return nil
		}

		if err := store.EraseDB(); err != nil {
			return fmt.Errorf("failed to erase database: %w", err)
		}
		fmt.Println("Successfully erased the CLI database.")
		return nil
	},
}

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage remote registries",
}

var registryAddCmd = &cobra.Command{
	Use:               "add [uri] [type] [priority] [name]",
	Aliases:           []string{"a"},
	Short:             "Add a new remote registry",
	Args:              cobra.RangeArgs(2, 4),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		uri := args[0]
		registryType := args[1]
		priority := 0
		name := ""
		if len(args) >= 3 {
			var err error
			priority, err = strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("failed to parse priority: %w", err)
			}
		}
		if len(args) == 4 {
			name = args[3]
		}
		// Validate registry type
		regType, err := models.MakeRegistryType(registryType)
		if err != nil {
			return fmt.Errorf("invalid registry type: %w", err)
		}

		// Check if registry with the same URI already exists
		registries, err := store.ListRegistries()
		if err != nil {
			return fmt.Errorf("failed to list registries: %w", err)
		}
		for _, r := range registries {
			if r.URI == uri {
				return fmt.Errorf("a registry with URI '%s' already exists", uri)
			}
		}

		// get registry info to validate it is reachable and working
		info, err := remote.GetRemoteInfo(models.RemoteRegistry{URI: uri, RegistryType: regType})
		if err != nil {
			return fmt.Errorf("failed to fetch registry info: %w", err)
		}

		if name == "" {
			name = info.DefaultName
		}

		if err := store.AddRegistry(uri, regType, priority, name); err != nil {
			return fmt.Errorf("failed to add registry: %w", err)
		}
		fmt.Printf("Successfully added registry '%s' of type '%s' with priority %d\n", uri, registryType, priority)
		return nil
	},
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all remote registries",
	RunE: func(cmd *cobra.Command, args []string) error {
		registries, err := store.ListRegistries()
		if err != nil {
			return fmt.Errorf("failed to list registries: %w", err)
		}
		fmt.Println("Remote Registries:")
		for _, r := range registries {
			fmt.Printf("- Name: \033[1m%s\033[0m, Type: %s, Priority: %d, URI: %s\n", r.Name, r.RegistryType, r.Priority, r.URI)
		}
		return nil
	},
}

var registryRemoveCmd = &cobra.Command{
	Use:               "remove [uri]",
	Aliases:           []string{"rm"},
	Short:             "Remove a remote registry",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		uri := args[0]
		rowsAffected, err := store.RemoveRegistry(uri)
		if err != nil {
			return fmt.Errorf("failed to remove registry: %w", err)
		}
		if rowsAffected == 0 {
			fmt.Printf("No registry found with URI '%s'\n", uri)
		} else {
			fmt.Printf("Successfully removed registry '%s'\n", uri)
		}
		return nil
	},
}

func updateOne(name string, runtime container.Runtime) error {
	pkgToUpdate, exists, err := store.SearchRemotePackageByName(name)
	if err != nil {
		return fmt.Errorf("failed to search remote package: %w", err)
	}
	if !exists {
		return fmt.Errorf("package '%s' not found in any remote registry", name)
	}

	fmt.Printf("Pulling latest image '%s' for CLI '%s' using %s...\n", pkgToUpdate.Container, name, runtime)
	runtimeCmd := exec.Command(string(runtime), "pull", pkgToUpdate.Container)
	runtimeCmd.Stdout = os.Stdout
	runtimeCmd.Stderr = os.Stderr

	if err := runtimeCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image with %s: %w", runtime, err)
	}
	fmt.Printf("Successfully pulled latest image '%s'\n", pkgToUpdate.Container)

	printKeyValueSection("Config mounts", pkgToUpdate.ConfigMounts)
	printKeyValueSection("Port mappings", pkgToUpdate.PortMappings)

	if err := store.UpdateCLI(name, pkgToUpdate); err != nil {
		return fmt.Errorf("failed to update CLI in db: %w", err)
	}

	return nil
}

var updateCmd = &cobra.Command{
	Use:     "update [name]",
	Aliases: []string{"u"},
	Short:   "Update a CLI configuration by pulling the latest container image",
	Long:    "Update a CLI configuration by pulling the latest container image.\nIf no name is given, every installed CLI is updated.",
	Args:    cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			return err
		}

		if len(args) == 1 {
			return updateOne(args[0], runtime)
		}

		clis, err := store.ListCLIs()
		if err != nil {
			return fmt.Errorf("failed to list CLIs: %w", err)
		}
		if len(clis) == 0 {
			fmt.Println("No CLIs installed.")
			return nil
		}

		var failed []string
		for i, c := range clis {
			fmt.Printf("[%d/%d] Updating '%s'...\n", i+1, len(clis), c.Name)
			if err := updateOne(c.Name, runtime); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to update '%s': %v\n", c.Name, err)
				failed = append(failed, c.Name)
			}
			fmt.Println()
		}

		succeeded := len(clis) - len(failed)
		fmt.Printf("Updated %d/%d CLIs.\n", succeeded, len(clis))
		if len(failed) > 0 {
			return fmt.Errorf("failed to update: %s", strings.Join(failed, ", "))
		}
		return nil
	},
}

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update the disguised-penguin CLI to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Fetch the latest release info from GitHub API
		apiURL := "https://api.github.com/repos/AlessandroRuggiero/disguised-penguin/releases/latest"
		respAPI, err := http.Get(apiURL)
		if err != nil {
			return fmt.Errorf("failed to fetch latest release info: %w", err)
		}
		defer respAPI.Body.Close()

		if respAPI.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to fetch latest release info: HTTP %d", respAPI.StatusCode)
		}

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(respAPI.Body).Decode(&release); err != nil {
			return fmt.Errorf("failed to parse release info: %w", err)
		}

		fmt.Printf("Current version: %s\n", Version)
		fmt.Printf("Latest version:  %s\n", release.TagName)

		if Version == release.TagName {
			fmt.Println("You are already on the latest version!")
			return nil
		}

		fmt.Println("Updating...")

		assetName := fmt.Sprintf("dp-%s-%s", runtime.GOOS, runtime.GOARCH)
		if runtime.GOOS == "windows" {
			assetName += ".exe"
		}
		url := fmt.Sprintf("https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest/download/%s", assetName)
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to download latest version: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to download %s: HTTP %d (this platform may not have a published release binary)", assetName, resp.StatusCode)
		}

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine executable path: %w", err)
		}
		exePath, err = filepath.EvalSymlinks(exePath)
		if err != nil {
			return fmt.Errorf("failed to resolve executable path: %w", err)
		}

		tempFile := exePath + ".new"
		out, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to create temporary file '%s': %w (you might need elevated privileges)", tempFile, err)
		}

		if _, err = io.Copy(out, resp.Body); err != nil {
			out.Close()
			os.Remove(tempFile)
			return fmt.Errorf("failed to write latest version: %w", err)
		}
		out.Close()

		if runtime.GOOS == "windows" {
			// Windows locks a running executable's file, so it can't be
			// overwritten or renamed-over directly like on POSIX. Move it
			// aside first, then move the new binary into place.
			oldFile := exePath + ".old"
			os.Remove(oldFile) // best-effort cleanup of a leftover from a previous update

			if err := os.Rename(exePath, oldFile); err != nil {
				os.Remove(tempFile)
				return fmt.Errorf("failed to move current executable aside: %w (you might need elevated privileges)", err)
			}
			if err := os.Rename(tempFile, exePath); err != nil {
				os.Rename(oldFile, exePath) // best-effort rollback
				return fmt.Errorf("failed to update executable: %w (you might need elevated privileges)", err)
			}
			// The old binary is still locked by this running process; clean it
			// up on a best-effort basis (it'll usually succeed once this
			// process exits and Windows releases the handle on next run).
			os.Remove(oldFile)
		} else {
			if err := os.Rename(tempFile, exePath); err != nil {
				os.Remove(tempFile)
				return fmt.Errorf("failed to update executable: %w (you might need elevated privileges)", err)
			}
		}

		fmt.Printf("Successfully updated 'dp' to %s.\n", release.TagName)
		return nil
	},
}

var registryVisitCmd = &cobra.Command{
	Use:     "visit [glob]",
	Aliases: []string{"v"},
	Short:   "Show the clis in one or more registries matching the given glob pattern",
	Example: `  dp registry visit "*"
  dp registry visit "local-*"
  dp registry visit "*dev*"`,
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r := "*"
		if len(args) >= 1 {
			r = args[0]
		}
		registries, err := store.GetRegistryByRegex(r)
		if err != nil {
			return fmt.Errorf("failed to get registries: %w", err)
		}
		if len(registries) == 0 {
			fmt.Printf("No registries found matching glob '%s'\n", r)
			return nil
		}

		for _, registry := range registries {
			// list all clis in the registry
			pkgs, err := remote.GetRemotePackages(registry)
			if err != nil {
				fmt.Printf("Failed to fetch packages from registry %s: %v\n", registry.URI, err)
				continue
			}
			fmt.Printf("Registry: %s (Type: %s, Priority: %d)\n", registry.Name, registry.RegistryType, registry.Priority)
			for pkgName, pkg := range pkgs {
				fmt.Printf("- \033[1m%s\033[0m (Container: %s)\n", pkgName, pkg.Container)
			}
		}
		return nil
	},
}

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Manage workspaces",
}

var workspaceAddCmd = &cobra.Command{
	Use:               "add [name]",
	Aliases:           []string{"a"},
	Short:             "Add a new workspace",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := store.AddWorkspace(name); err != nil {
			return fmt.Errorf("failed to add workspace: %w", err)
		}
		fmt.Printf("Successfully added workspace '%s'\n", name)
		return nil
	},
}

var workspaceRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm", "r"},
	Short:   "Remove a workspace",
	Args:    cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		workspaces, err := store.ListWorkspaces()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, w := range workspaces {
			names = append(names, w.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name == "default" {
			return fmt.Errorf("cannot remove the 'default' workspace")
		}

		ok, err := store.RemoveWorkspace(name)
		if err != nil {
			return fmt.Errorf("failed to remove workspace: %w", err)
		}
		if !ok {
			fmt.Printf("No workspace found with name '%s'\n", name)
			return nil
		}

		ws, err := workspace.Get(name)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace '%s': %w", name, err)
		}
		if err := ws.Delete(); err != nil {
			return fmt.Errorf("failed to delete on-disk data for workspace '%s': %w", name, err)
		}

		fmt.Printf("Successfully removed workspace '%s' and its on-disk data\n", name)
		return nil
	},
}

var workspaceCleanCmd = &cobra.Command{
	Use:     "clean [workspace] [cli]",
	Aliases: []string{"c"},
	Short:   "Delete a single CLI's on-disk data from a workspace",
	Args:    cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			workspaces, err := store.ListWorkspaces()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			var names []string
			for _, w := range workspaces {
				names = append(names, w.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		case 1:
			clis, err := store.ListCLIs()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			var names []string
			for _, c := range clis {
				names = append(names, c.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceName := args[0]
		cliName := args[1]

		if _, exists, err := store.GetWorkspaceByName(workspaceName); err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found", workspaceName)
		}

		ws, err := workspace.Get(workspaceName)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace '%s': %w", workspaceName, err)
		}

		if _, err := os.Stat(ws.CLIDir(cliName)); os.IsNotExist(err) {
			fmt.Printf("No data found for CLI '%s' in workspace '%s'\n", cliName, workspaceName)
			return nil
		}

		if err := ws.DeleteCLI(cliName); err != nil {
			return fmt.Errorf("failed to delete data for CLI '%s' in workspace '%s': %w", cliName, workspaceName, err)
		}

		fmt.Printf("Successfully deleted data for CLI '%s' in workspace '%s'\n", cliName, workspaceName)
		return nil
	},
}

var workspaceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all workspaces",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaces, err := store.ListWorkspaces()
		if err != nil {
			return err
		}
		if len(workspaces) == 0 {
			fmt.Println("No workspaces found.")
			return nil
		}
		fmt.Println("Workspaces:")
		for _, w := range workspaces {
			fmt.Printf("- %s\n", w.Name)
		}
		return nil
	},
}

var workspaceProtectionCmd = &cobra.Command{
	Use:     "protection",
	Aliases: []string{"prot"},
	Short:   "Manage mount protections for a workspace",
	Long:    "Manage mount protections for a workspace.\nA protection remounts a path within the workspace read-only (ro), writable (rw), or hidden (h) whenever a CLI runs against that workspace.",
}

var workspaceProtectionAddCmd = &cobra.Command{
	Use:     "add [workspace] [path:mode]",
	Aliases: []string{"a"},
	Short:   "Protect a path within a workspace (mode: ro, rw, or h)",
	Long:    "Protect a path within a workspace so it is remounted read-only (ro), writable (rw), or hidden (h) whenever a CLI runs against that workspace.\nThe path is relative to the workspace root. Re-adding an existing path updates its mode.",
	Example: `  dp workspace protection add default .git:ro
  dp ws protection add dev .env:h`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		workspaces, err := store.ListWorkspaces()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, w := range workspaces {
			names = append(names, w.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceName := args[0]

		prot, err := container.ParseProtection(args[1])
		if err != nil {
			return err
		}

		ws, exists, err := store.GetWorkspaceByName(workspaceName)
		if err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found (create it with 'dp workspace add %s')", workspaceName, workspaceName)
		}

		if err := store.AddMountProtection(ws.ID, prot.Rel, prot.Mode); err != nil {
			return fmt.Errorf("failed to add mount protection: %w", err)
		}
		fmt.Printf("Protected '%s' as '%s' in workspace '%s'\n", prot.Rel, prot.Mode, workspaceName)
		return nil
	},
}

var workspaceProtectionRemoveCmd = &cobra.Command{
	Use:     "remove [workspace] [path]",
	Aliases: []string{"rm", "r"},
	Short:   "Remove a mount protection from a workspace",
	Example: `  dp workspace protection remove default .git
  dp ws protection rm dev .env`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			workspaces, err := store.ListWorkspaces()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			var names []string
			for _, w := range workspaces {
				names = append(names, w.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		case 1:
			ws, exists, err := store.GetWorkspaceByName(args[0])
			if err != nil || !exists {
				return nil, cobra.ShellCompDirectiveError
			}
			protections, err := store.GetMountProtections(ws.ID)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			var paths []string
			for _, p := range protections {
				paths = append(paths, p.MountPath)
			}
			return paths, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceName := args[0]
		// Clean the path the same way ParseProtection stored it, so ".git",
		// "./.git" and ".git/" all match the recorded protection.
		mountPath := path.Clean(args[1])

		ws, exists, err := store.GetWorkspaceByName(workspaceName)
		if err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found", workspaceName)
		}

		removed, err := store.RemoveMountProtection(ws.ID, mountPath)
		if err != nil {
			return fmt.Errorf("failed to remove mount protection: %w", err)
		}
		if !removed {
			fmt.Printf("No protection found for '%s' in workspace '%s'\n", mountPath, workspaceName)
			return nil
		}
		fmt.Printf("Removed protection for '%s' in workspace '%s'\n", mountPath, workspaceName)
		return nil
	},
}

var workspaceProtectionListCmd = &cobra.Command{
	Use:     "list [workspace]",
	Aliases: []string{"ls"},
	Short:   "List mount protections for a workspace",
	Args:    cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		workspaces, err := store.ListWorkspaces()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, w := range workspaces {
			names = append(names, w.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceName := args[0]

		ws, exists, err := store.GetWorkspaceByName(workspaceName)
		if err != nil {
			return fmt.Errorf("failed to look up workspace '%s': %w", workspaceName, err)
		} else if !exists {
			return fmt.Errorf("workspace '%s' not found", workspaceName)
		}

		protections, err := store.GetMountProtections(ws.ID)
		if err != nil {
			return fmt.Errorf("failed to get mount protections: %w", err)
		}
		if len(protections) == 0 {
			fmt.Printf("No protections set for workspace '%s'\n", workspaceName)
			return nil
		}
		fmt.Printf("Protections in workspace '%s':\n", workspaceName)
		for _, p := range protections {
			fmt.Printf("- %s -> %s\n", p.MountPath, p.Permission)
		}
		return nil
	},
}
