package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"disguised-penguin/internal/db"

	"github.com/spf13/cobra"
)

var store *db.Store

func SetupBindings(dbStore *db.Store) {
	store = dbStore
}

var rootCmd = &cobra.Command{
	Use:                "dp  [cli_name] [args...]",
	Short:              "Run CLI applications in a containerized environment",
	Long:               ``,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cliName := args[0]
		cli, err := store.GetCliByName(cliName)
		if err != nil {
			return fmt.Errorf("failed to get CLI: %w", err)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		dockerArgs := []string{"run", "--rm", "-it", "-v", fmt.Sprintf("%s:/workspace", cwd), "-w", "/workspace"}

		if currentUser, err := user.Current(); err == nil {
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("PUID=%s", currentUser.Uid))
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("PGID=%s", currentUser.Gid))
		} else {
			fmt.Printf("Warning: Could not get current user info: %v. Container may run as root.\n", err)
		}

		appDataDir, err := db.GetDBPath()
		if err != nil {
			return fmt.Errorf("failed to get app data dir: %w", err)
		}
		volumesDir := filepath.Join(filepath.Dir(appDataDir), "volumes", cli.Name)

		for volumeName, containerPath := range cli.ConfigMounts {
			hostVolumePath := filepath.Join(volumesDir, volumeName)
			if err := os.MkdirAll(hostVolumePath, 0755); err != nil {
				return fmt.Errorf("failed to create host volume directory: %w", err)
			}
			dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s:%s", hostVolumePath, containerPath))
		}

		for hostPort, containerPort := range cli.PortMappings {
			dockerArgs = append(dockerArgs, "-p", fmt.Sprintf("%s:%s", hostPort, containerPort))
		}

		dockerArgs = append(dockerArgs, cli.ContainerName)
		dockerArgs = append(dockerArgs, args[1:]...)
		dockerCmd := exec.Command("docker", dockerArgs...)
		dockerCmd.Stdin = os.Stdin
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr

		if err := dockerCmd.Run(); err != nil {
			return fmt.Errorf("failed to run docker container: %w", err)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(eraseDBCmd)
	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryRemoveCmd)
}
