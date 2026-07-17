package workspace

import (
	"os"
	"path/filepath"

	"disguised-penguin/internal/db"
)

type Workspace struct {
	Name string
	dir  string
}

func Get(name string) (*Workspace, error) {
	dbPath, err := db.GetDBPath()
	if err != nil {
		return nil, err
	}
	return &Workspace{
		Name: name,
		dir:  filepath.Join(filepath.Dir(dbPath), "workspaces", name),
	}, nil
}

func (w *Workspace) Dir() string {
	return w.dir
}

func (w *Workspace) CLIDir(cliName string) string {
	return filepath.Join(w.dir, "volumes", cliName)
}

func (w *Workspace) VolumeDir(cliName, volumeName string) string {
	return filepath.Join(w.CLIDir(cliName), volumeName)
}

func (w *Workspace) Delete() error {
	return os.RemoveAll(w.dir)
}

func (w *Workspace) DeleteCLI(cliName string) error {
	return os.RemoveAll(w.CLIDir(cliName))
}
