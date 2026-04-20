package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:     "add [name] [container_name]",
	Aliases: []string{"a"},
	Short:   "Add a new CLI configuration to the database",
	Args:    cobra.ExactArgs(2),
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
	Use:     "rm [name]",
	Aliases: []string{"remove", "r"},
	Short:   "Remove a CLI configuration from the database",
	Args:    cobra.ExactArgs(1),
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
	Use:     "install [name]",
	Aliases: []string{"i"},
	Short:   "Install a CLI configuration by pulling the associated Docker image",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		pkgToInstall, exists, err := store.SearchRemotePackageByName(name)
		if err != nil {
			return fmt.Errorf("failed to search remote package: %w", err)
		}
		if !exists {
			return fmt.Errorf("package '%s' not found in any remote registry", name)
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
