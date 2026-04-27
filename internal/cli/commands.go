package cli

import (
	"disguised-penguin/internal/models"
	"disguised-penguin/internal/remote"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"
)

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
		rowsAffected, err := store.RemoveCLI(name)
		if err != nil {
			return fmt.Errorf("failed to delete CLI from db: %w", err)
		}
		if rowsAffected == 0 {
			fmt.Printf("No CLI found with name '%s'\n", name)
		} else {
			fmt.Printf("Successfully removed CLI '%s'\n", name)
		}
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:               "install [name]",
	Aliases:           []string{"i"},
	Short:             "Install a CLI configuration by pulling the associated Docker image",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		pkgToInstall, exists, err := store.SearchRemotePackageByName(name)
		if err != nil {
			return fmt.Errorf("failed to search remote package: %w", err)
		}
		if !exists {
			return fmt.Errorf("package '%s' not found in any remote registry", name)
		}

		// make sure it is not already installed
		if _, err := store.GetCliByName(name); err == nil {
			return fmt.Errorf("CLI '%s' is already installed", name)
		}

		fmt.Printf("Pulling Docker image '%s' for CLI '%s'...\n", pkgToInstall.Container, name)
		dockerCmd := exec.Command("docker", "pull", pkgToInstall.Container)
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr

		if err := dockerCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull docker image: %w", err)
		}
		fmt.Printf("Successfully pulled Docker image '%s'\n", pkgToInstall.Container)

		fmt.Println("Config mounts:", pkgToInstall.ConfigMounts)
		fmt.Println("Port mappings:", pkgToInstall.PortMappings)

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

var updateCmd = &cobra.Command{
	Use:     "update [name]",
	Aliases: []string{"u"},
	Short:   "Update a CLI configuration by pulling the latest Docker image",
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
		pkgToUpdate, exists, err := store.SearchRemotePackageByName(name)
		if err != nil {
			return fmt.Errorf("failed to search remote package: %w", err)
		}
		if !exists {
			return fmt.Errorf("package '%s' not found in any remote registry", name)
		}

		fmt.Printf("Pulling latest Docker image '%s' for CLI '%s'...\n", pkgToUpdate.Container, name)
		dockerCmd := exec.Command("docker", "pull", pkgToUpdate.Container)
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr

		if err := dockerCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull docker image: %w", err)
		}
		fmt.Printf("Successfully pulled latest Docker image '%s'\n", pkgToUpdate.Container)

		fmt.Println("Config mounts:", pkgToUpdate.ConfigMounts)
		fmt.Println("Port mappings:", pkgToUpdate.PortMappings)

		if err := store.UpdateCLI(name, pkgToUpdate); err != nil {
			return fmt.Errorf("failed to update CLI in db: %w", err)
		}

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
