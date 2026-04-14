package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

const remoteRepoURL string = "https://raw.githubusercontent.com/AlessandroRuggiero/disguised-penguin-repo/main"

var db *sql.DB

type CLI struct {
	ID            int
	Name          string
	ContainerName string
	ConfigMounts  map[string]string
	PortMappings  map[string]string
}

type RemotePackage struct {
	Container    string            `json:"container"`
	ConfigMounts map[string]string `json:"configmounts,omitempty"`
	PortMappings map[string]string `json:"portmappings,omitempty"`
}

func getRemotePackages() (map[string]RemotePackage, error) {
	// fetch the pkgs.json from the remote repo
	resp, err := http.Get(remoteRepoURL + "/pkgs.json")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote packages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pkgs map[string]RemotePackage
	if err := json.NewDecoder(resp.Body).Decode(&pkgs); err != nil {
		return nil, fmt.Errorf("failed to decode remote packages: %w", err)
	}
	return pkgs, nil
}

func GetDBPath() (string, error) {
	// Get XDG_DATA_HOME or default to ~/.local/share
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		xdgDataHome = filepath.Join(home, ".local", "share")
	}

	// Create app data directory
	appDir := filepath.Join(xdgDataHome, "disguised-penguin")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", err
	}

	return filepath.Join(appDir, "data.db"), nil
}

func getCliByName(name string) (*CLI, error) {
	var cli CLI
	var configMountsStr string
	var portMappingsStr string
	err := db.QueryRow(`SELECT id, name, container_name, config_mounts, port_mappings FROM clis WHERE name = ?`, name).Scan(&cli.ID, &cli.Name, &cli.ContainerName, &configMountsStr, &portMappingsStr)
	// if the error is sql.ErrNoRows, it means the CLI was not found in the database, so we return a more user-friendly error message
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("CLI '%s' not found in database.\nSuggestion: Use 'dp list' to see available CLIs or 'dp install %s' to install it from the remote repository", name, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query CLI: %w", err)
	}
	if err := json.Unmarshal([]byte(configMountsStr), &cli.ConfigMounts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config mounts: %w", err)
	}
	if err := json.Unmarshal([]byte(portMappingsStr), &cli.PortMappings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal port mappings: %w", err)
	}
	return &cli, nil
}

var rootCmd = &cobra.Command{
	Use:   "dp  [cli_name] [args...]",
	Short: "Run CLI applications in a containerized environment",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	// Allow unknown flags to pass through to the target CLI
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cliName := args[0]
		cli, err := getCliByName(cliName)
		if err != nil {
			return fmt.Errorf("failed to get CLI: %w", err)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// fmt.Printf("Running CLI '%s' in container '%s' in the current directory '%s'\n", cli.Name, cli.ContainerName, cwd)

		dockerArgs := []string{"run", "--rm", "-it", "-v", fmt.Sprintf("%s:/workspace", cwd), "-w", "/workspace"}
		for volumeName, containerPath := range cli.ConfigMounts {
			configVolume := fmt.Sprintf("%s___%s", cli.Name, volumeName)
			dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s:%s", configVolume, containerPath))
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

var addCmd = &cobra.Command{
	Use:     "add [name] [container_name]",
	Aliases: []string{"a"},
	Short:   "Add a new CLI configuration to the database",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		containerName := args[1]
		_, err := db.Exec(`INSERT INTO clis (name, container_name, config_mounts, port_mappings) VALUES (?, ?, ?, ?)`, name, containerName, "{}", "{}")
		if err != nil {
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

		result, err := db.Exec(`DELETE FROM clis WHERE name = ?`, name)
		if err != nil {
			return fmt.Errorf("failed to delete CLI from db: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
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
		remotePkgs, err := getRemotePackages()
		if err != nil {
			return fmt.Errorf("failed to get remote packages: %w", err)
		}
		name := args[0]
		pkgToInstall, exists := remotePkgs[name]
		if !exists {
			return fmt.Errorf("package '%s' not found in remote repository", name)
		}

		fmt.Printf("Pulling Docker image '%s' for CLI '%s'...\n", pkgToInstall.Container, name)
		dockerCmd := exec.Command("docker", "pull", pkgToInstall.Container)
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr

		if err := dockerCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull docker image: %w", err)
		}
		fmt.Printf("Successfully pulled Docker image '%s'\n", pkgToInstall.Container)

		// Add to local database
		configMountsBytes, err := json.Marshal(pkgToInstall.ConfigMounts)
		fmt.Println("Config mounts:", pkgToInstall.ConfigMounts)
		if err != nil {
			return fmt.Errorf("failed to marshal config mounts: %w", err)
		}
		portMappingsBytes, err := json.Marshal(pkgToInstall.PortMappings)
		fmt.Println("Port mappings:", pkgToInstall.PortMappings)
		if err != nil {
			return fmt.Errorf("failed to marshal port mappings: %w", err)
		}
		_, err = db.Exec(`INSERT INTO clis (name, container_name, config_mounts, port_mappings) VALUES (?, ?, ?, ?)`, name, pkgToInstall.Container, string(configMountsBytes), string(portMappingsBytes))
		if err != nil {
			return fmt.Errorf("failed to insert CLI into db: %w", err)
		}

		return nil
	},
}

var eraseDBCmd = &cobra.Command{
	Use:   "erase-db",
	Short: "Erase the entire CLI database (use with caution)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// make the user confirm before erasing the database
		fmt.Print("Are you sure you want to erase the entire CLI database? This action cannot be undone. (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborting database erase.")
			return nil
		}

		// close the current DB connection before deleting the file
		db.Close()

		dbPath, err := GetDBPath()
		if err != nil {
			return fmt.Errorf("could not get DB path: %w", err)
		}
		if err := os.Remove(dbPath); err != nil {
			return fmt.Errorf("failed to erase database: %w", err)
		}
		fmt.Println("Successfully erased the CLI database.")
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available CLIs in the database",
	RunE: func(cmd *cobra.Command, args []string) error {
		rows, err := db.Query(`SELECT name, container_name FROM clis`)
		if err != nil {
			return fmt.Errorf("failed to query CLIs: %w", err)
		}
		defer rows.Close()

		fmt.Println("Available CLIs:")
		for rows.Next() {
			var name, containerName string
			if err := rows.Scan(&name, &containerName); err != nil {
				return fmt.Errorf("failed to scan row: %w", err)
			}
			fmt.Printf("- %s (container: %s)\n", name, containerName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(eraseDBCmd)
}

func main() {
	dbPath, err := GetDBPath()
	if err != nil {
		fmt.Printf("Could not get DB path: %v\n", err)
		os.Exit(1)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("Failed to open DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize simple key-value table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS clis (
		id INTEGER PRIMARY KEY AUTOINCREMENT, 
		name TEXT UNIQUE, 
		container_name TEXT,
		config_mounts TEXT,
		port_mappings TEXT
	)`)
	if err != nil {
		fmt.Printf("Failed to create table: %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
