package db

import (
	"database/sql"
	"disguised-penguin/internal/models"
	"disguised-penguin/internal/remote"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db     *sql.DB
	dbPath string
}

func GetDBPath() (string, error) {
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		xdgDataHome = filepath.Join(home, ".local", "share")
	}

	appDir := filepath.Join(xdgDataHome, "disguised-penguin")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", err
	}

	return filepath.Join(appDir, "data.db"), nil
}

func NewStore() (*Store, error) {
	dbPath, err := GetDBPath()
	if err != nil {
		return nil, fmt.Errorf("could not get DB path: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open DB: %w", err)
	}

	store := &Store{db: db, dbPath: dbPath}
	if err := store.RunMigrations(); err != nil {
		store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) RunMigrations() error {
	var currentVersion int
	if err := s.db.QueryRow("PRAGMA user_version;").Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to check current db version: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort files to ensure strict version ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		parts := strings.Split(name, "_")
		if len(parts) < 2 {
			continue
		}

		migVersion, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration file prefix (must be integer): %s", name)
		}

		if migVersion > currentVersion {
			content, err := migrationFiles.ReadFile(filepath.Join("migrations", name))
			if err != nil {
				return fmt.Errorf("failed to read migration %s: %w", name, err)
			}

			// Execute migration
			if _, err := s.db.Exec(string(content)); err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", name, err)
			}

			// Update user_version
			if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", migVersion)); err != nil {
				return fmt.Errorf("failed to update db version after migration %s: %w", name, err)
			}
		}
	}

	return nil
}

func (s *Store) GetCliByName(name string) (*models.CLI, error) {
	var cli models.CLI
	var configMountsStr string
	var portMappingsStr string
	err := s.db.QueryRow(`SELECT id, name, container_name, config_mounts, port_mappings FROM clis WHERE name = ?`, name).Scan(&cli.ID, &cli.Name, &cli.ContainerName, &configMountsStr, &portMappingsStr)
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

func (s *Store) AddCLI(name, containerName string) error {
	_, err := s.db.Exec(`INSERT INTO clis (name, container_name, config_mounts, port_mappings) VALUES (?, ?, ?, ?)`, name, containerName, "{}", "{}")
	return err
}

func (s *Store) RemoveCLI(name string) (int64, error) {
	result, err := s.db.Exec(`DELETE FROM clis WHERE name = ?`, name)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) InstallCLI(name string, pkg *models.RemotePackage) error {
	configMountsBytes, err := json.Marshal(pkg.ConfigMounts)
	if err != nil {
		return fmt.Errorf("failed to marshal config mounts: %w", err)
	}
	portMappingsBytes, err := json.Marshal(pkg.PortMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal port mappings: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO clis (name, container_name, config_mounts, port_mappings) VALUES (?, ?, ?, ?)`, name, pkg.Container, string(configMountsBytes), string(portMappingsBytes))
	return err
}

func (s *Store) EraseDB() error {
	s.Close()
	return os.Remove(s.dbPath)
}

func (s *Store) ListCLIs() ([]models.CLI, error) {
	rows, err := s.db.Query(`SELECT name, container_name FROM clis`)
	if err != nil {
		return nil, fmt.Errorf("failed to query CLIs: %w", err)
	}
	defer rows.Close()

	var clis []models.CLI
	for rows.Next() {
		var name, containerName string
		if err := rows.Scan(&name, &containerName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		clis = append(clis, models.CLI{Name: name, ContainerName: containerName})
	}
	return clis, nil
}

func (s *Store) GetRegistryByRegex(pattern string) ([]models.RemoteRegistry, error) {
	// examples: "def*" to match all registries starting with "def", "*hub" to match all registries ending with "hub", "*repo*" to match all registries containing "repo"
	rows, err := s.db.Query(`SELECT uri, registry_type, priority, name FROM registries WHERE name GLOB ?`, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to query registries: %w", err)
	}
	defer rows.Close()

	var registries []models.RemoteRegistry
	for rows.Next() {
		var uri, registryTypeStr, name string
		var priority int
		if err := rows.Scan(&uri, &registryTypeStr, &priority, &name); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		registryType := models.RegistryType(registryTypeStr)
		registries = append(registries, models.RemoteRegistry{URI: uri, RegistryType: registryType, Priority: priority, Name: name})
	}
	return registries, nil
}

func (s *Store) ListRegistries() ([]models.RemoteRegistry, error) {
	rows, err := s.db.Query(`SELECT uri, registry_type, priority, name FROM registries ORDER BY priority DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query registries: %w", err)
	}
	defer rows.Close()

	var registries []models.RemoteRegistry
	for rows.Next() {
		var uri, registryTypeStr, name string
		var priority int
		if err := rows.Scan(&uri, &registryTypeStr, &priority, &name); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		registryType := models.RegistryType(registryTypeStr)
		registries = append(registries, models.RemoteRegistry{URI: uri, RegistryType: registryType, Priority: priority, Name: name})
	}
	return registries, nil
}

func (s *Store) SearchRemotePackageByName(name string) (*models.RemotePackage, bool, error) {
	registries, err := s.ListRegistries()
	if err != nil {
		return nil, false, fmt.Errorf("failed to get remote registries: %w", err)
	}

	for _, registry := range registries {
		pkgs, err := remote.GetRemotePackages(registry)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch packages from registry %s: %v\n", registry.URI, err)
			continue
		}
		if pkg, exists := pkgs[name]; exists {
			return &pkg, true, nil
		}
	}
	return nil, false, fmt.Errorf("package '%s' not found in any remote registry", name)
}

func (s *Store) RemoveRegistry(uri string) (int64, error) {
	result, err := s.db.Exec(`DELETE FROM registries WHERE uri = ?`, uri)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) AddRegistry(uri string, registryType models.RegistryType, priority int, name string) error {
	_, err := s.db.Exec(`INSERT INTO registries (uri, registry_type, priority, name) VALUES (?, ?, ?, ?)`, uri, string(registryType), priority, name)
	return err
}

func (s *Store) UpdateCLI(name string, pkg *models.RemotePackage) error {
	configMountsBytes, err := json.Marshal(pkg.ConfigMounts)
	if err != nil {
		return fmt.Errorf("failed to marshal config mounts: %w", err)
	}
	portMappingsBytes, err := json.Marshal(pkg.PortMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal port mappings: %w", err)
	}
	_, err = s.db.Exec(`UPDATE clis SET container_name = ?, config_mounts = ?, port_mappings = ? WHERE name = ?`, pkg.Container, string(configMountsBytes), string(portMappingsBytes), name)
	return err
}

func (s *Store) GetWorkspaceByName(name string) (*models.Workspace, bool, error) {
	var workspace models.Workspace
	err := s.db.QueryRow(`SELECT id, name FROM workspaces WHERE name = ?`, name).Scan(&workspace.ID, &workspace.Name)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to query workspace: %w", err)
	}
	return &workspace, true, nil
}

func (s *Store) AddWorkspace(name string) error {
	_, err := s.db.Exec(`INSERT INTO workspaces (name) VALUES (?)`, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("workspace '%s' already exists", name)
		}
		return err
	}
	return nil
}

func (s *Store) RemoveWorkspace(name string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM workspaces WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (s *Store) ListWorkspaces() ([]models.Workspace, error) {
	rows, err := s.db.Query(`SELECT id, name FROM workspaces`)
	if err != nil {
		return nil, fmt.Errorf("failed to query workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []models.Workspace
	for rows.Next() {
		var ws models.Workspace
		if err := rows.Scan(&ws.ID, &ws.Name); err != nil {
			return nil, fmt.Errorf("failed to scan workspace row: %w", err)
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}
