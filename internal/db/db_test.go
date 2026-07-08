package db

import (
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
