package db

import (
	"database/sql"
	"disguised-penguin/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func withGOOS(t *testing.T, value string) func() {
	prev := goos
	goos = value
	return func() { goos = prev }
}

func TestGetDBPath_XDGDataHomeOverridesEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("LOCALAPPDATA", "should-be-ignored")
	defer withGOOS(t, "windows")()

	path, err := GetDBPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "disguised-penguin", "data.db")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestGetDBPath_WindowsUsesLocalAppData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", dir)
	defer withGOOS(t, "windows")()

	path, err := GetDBPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "disguised-penguin", "data.db")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("expected app dir to be created: %v", err)
	}
}

func TestGetDBPath_WindowsFallsBackToHomeAppDataLocalWhenUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("HOME", home)        // consulted by os.UserHomeDir on unix
	t.Setenv("USERPROFILE", home) // consulted by os.UserHomeDir on windows
	defer withGOOS(t, "windows")()

	path, err := GetDBPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "AppData", "Local", "disguised-penguin", "data.db")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestGetDBPath_NonWindowsUsesXDGDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)
	defer withGOOS(t, "linux")()

	path, err := GetDBPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "disguised-penguin", "data.db")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

// newTestStore points the store at an isolated data dir and runs migrations.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestInstallCLIRoundTripsExtraRunArgs(t *testing.T) {
	store := newTestStore(t)

	pkg := &models.RemotePackage{
		Image: "ghcr.io/foo/bar:latest",
		ExtraRunArgs: []models.ExtraRunArg{
			{Args: []string{"--cap-add", "SYS_PTRACE"}, Description: "attach to processes"},
		},
	}
	if err := store.InstallCLI("bar", pkg); err != nil {
		t.Fatalf("InstallCLI: %v", err)
	}

	cli, err := store.GetCliByName("bar")
	if err != nil {
		t.Fatalf("GetCliByName: %v", err)
	}
	if len(cli.ExtraRunArgs) != 1 {
		t.Fatalf("expected 1 extra run arg, got %d", len(cli.ExtraRunArgs))
	}
	if got, want := cli.ExtraRunArgs[0].String(), "--cap-add SYS_PTRACE"; got != want {
		t.Errorf("args: got %q, want %q", got, want)
	}
	if got, want := cli.ExtraRunArgs[0].Description, "attach to processes"; got != want {
		t.Errorf("description: got %q, want %q", got, want)
	}

	// Updating to a package without extra args clears them rather than
	// leaving the previously accepted ones behind.
	if err := store.UpdateCLI("bar", &models.RemotePackage{Image: pkg.Image}); err != nil {
		t.Fatalf("UpdateCLI: %v", err)
	}
	cli, err = store.GetCliByName("bar")
	if err != nil {
		t.Fatalf("GetCliByName after update: %v", err)
	}
	if len(cli.ExtraRunArgs) != 0 {
		t.Errorf("expected extra run args to be cleared, got %v", cli.ExtraRunArgs)
	}
	if raw := extraRunArgsColumn(t, store, "bar"); raw.Valid {
		t.Errorf("expected extra_run_args to be reset to NULL, got %q", raw.String)
	}
}

// extraRunArgsColumn reads the raw column so tests can tell NULL ("asked for
// nothing") apart from an empty JSON list.
func extraRunArgsColumn(t *testing.T, store *Store, name string) sql.NullString {
	t.Helper()
	var raw sql.NullString
	if err := store.db.QueryRow(`SELECT extra_run_args FROM clis WHERE name = ?`, name).Scan(&raw); err != nil {
		t.Fatalf("read extra_run_args for %q: %v", name, err)
	}
	return raw
}

func TestAddCLIHasNoExtraRunArgs(t *testing.T) {
	store := newTestStore(t)

	if err := store.AddCLI("manual", "ghcr.io/foo/manual:latest"); err != nil {
		t.Fatalf("AddCLI: %v", err)
	}
	cli, err := store.GetCliByName("manual")
	if err != nil {
		t.Fatalf("GetCliByName: %v", err)
	}
	if len(cli.ExtraRunArgs) != 0 {
		t.Errorf("expected no extra run args, got %v", cli.ExtraRunArgs)
	}
	// NULL, not "[]": the column is only populated when a package asks for
	// something, which is what makes its presence meaningful.
	if raw := extraRunArgsColumn(t, store, "manual"); raw.Valid {
		t.Errorf("expected extra_run_args to be NULL, got %q", raw.String)
	}
}

func TestInstallCLIWithoutExtraRunArgsStoresNull(t *testing.T) {
	store := newTestStore(t)

	if err := store.InstallCLI("plain", &models.RemotePackage{Image: "ghcr.io/foo/plain:latest"}); err != nil {
		t.Fatalf("InstallCLI: %v", err)
	}
	if raw := extraRunArgsColumn(t, store, "plain"); raw.Valid {
		t.Errorf("expected extra_run_args to be NULL, got %q", raw.String)
	}
}

// A database created before 003 must gain the column and read back as empty,
// not fail every query.
func TestMigrationAddsExtraRunArgsToExistingDB(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dbPath, err := GetDBPath()
	if err != nil {
		t.Fatalf("GetDBPath: %v", err)
	}

	// Stand up a v1-era database: the initial schema, no extra_run_args.
	old, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	initSQL, err := migrationFiles.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read 001: %v", err)
	}
	if _, err := old.Exec(string(initSQL)); err != nil {
		t.Fatalf("apply 001: %v", err)
	}
	if _, err := old.Exec(`PRAGMA user_version = 1;`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO clis (name, container_name, config_mounts, port_mappings) VALUES ('legacy', 'img', '{}', '{}')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore (migrating): %v", err)
	}
	defer store.Close()

	cli, err := store.GetCliByName("legacy")
	if err != nil {
		t.Fatalf("GetCliByName: %v", err)
	}
	if len(cli.ExtraRunArgs) != 0 {
		t.Errorf("expected no extra run args on a migrated row, got %v", cli.ExtraRunArgs)
	}
}
