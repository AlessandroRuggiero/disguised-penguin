package models

import (
	"encoding/json"
	"fmt"
)

type RegistryType string

const (
	RegistryTypeGitHub RegistryType = "github"
)

type CLI struct {
	ID           int
	Name         string
	Image        string
	ConfigMounts map[string]ConfigMount
	PortMappings map[string]string
}

type RemotePackage struct {
	// The JSON field stays "container" so already-published registries keep working.
	Image        string                 `json:"container"`
	ConfigMounts map[string]ConfigMount `json:"configmounts,omitempty"`
	PortMappings map[string]string      `json:"portmappings,omitempty"`
}

type ConfigMount struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (m ConfigMount) IsFile() bool { return m.Type == "file" }

// UnmarshalJSON accepts either a bare path string (a directory mount, the
// original format) or an object {"path": "...", "type": "file"|"dir"}.
func (m *ConfigMount) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Path = s
		m.Type = "dir"
		return nil
	}
	// rawMount has the same fields but no custom UnmarshalJSON, so decoding
	// into it doesn't recurse back here.
	type rawMount ConfigMount
	var r rawMount
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("config mount must be a path string or {path, type} object: %w", err)
	}
	if r.Path == "" {
		return fmt.Errorf("config mount object is missing \"path\"")
	}
	switch r.Type {
	case "", "dir", "directory":
		r.Type = "dir"
	case "file":
	default:
		return fmt.Errorf("invalid config mount type %q (expected \"file\" or \"dir\")", r.Type)
	}
	*m = ConfigMount(r)
	return nil
}

type LocalMount struct {
	Source string `json:"source,omitempty"`
	Path   string `json:"path"`
	Mode   string `json:"mode"` // Mode is "ro", "rw", or "h" (hidden). "h" is not valid with a Source.
}

type LocalMountConfigFile struct {
	Mounts []LocalMount `json:"mounts"`
}

type RemoteRegistry struct {
	URI          string
	RegistryType RegistryType
	Priority     int
	Name         string
}

type RemotePackageInfo struct {
	DefaultName string  `json:"default_name"`
	Description *string `json:"description"`
}

type Workspace struct {
	ID   int
	Name string
}

type MountProtection struct {
	ID          int
	WorkspaceID int
	MountPath   string
	Permission  string
}

type Variant struct {
	Of          string            `json:"of"`
	BuildFile   string            `json:"build_file"`
	LocalMounts map[string]string `json:"local_mounts,omitempty"`
}

type FileVariant struct {
	BuildFile   string            `json:"build_file"`
	LocalMounts map[string]string `json:"local_mounts,omitempty"`
}

type VariantConfigFile struct {
	Variants map[string]FileVariant `json:"variants"`
}

func MakeRegistryType(s string) (RegistryType, error) {
	switch s {
	case "github":
		return RegistryTypeGitHub, nil
	default:
		return "", fmt.Errorf("invalid registry type: %s", s)
	}
}

func (rt RegistryType) String() string {
	return string(rt)
}

func (mp MountProtection) String() string {
	return fmt.Sprintf("%s:%s", mp.MountPath, mp.Permission)
}
