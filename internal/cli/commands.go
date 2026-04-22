package cli

import (
	"disguised-penguin/internal/models"
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
	Use:               "remove [name]",
	Aliases:           []string{"rm", "r"},
	Short:             "Remove a CLI configuration from the database",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
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
	Use:               "add [uri] [type] [priority]",
	Aliases:           []string{"a"},
	Short:             "Add a new remote registry",
	Args:              cobra.RangeArgs(2, 3),
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		uri := args[0]
		registryType := args[1]
		priority := 0
		if len(args) == 3 {
			var err error
			priority, err = strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("failed to parse priority: %w", err)
			}
		}
		// Validate registry type
		regType, err := models.MakeRegistryType(registryType)
		if err != nil {
			return fmt.Errorf("invalid registry type: %w", err)
		}
		if err := store.AddRegistry(uri, regType, priority); err != nil {
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
			fmt.Printf("- URI: %s, Type: %s, Priority: %d\n", r.URI, r.RegistryType, r.Priority)
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
	Use:               "update [name]",
	Aliases:           []string{"u"},
	Short:             "Update a CLI configuration by pulling the latest Docker image",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
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
