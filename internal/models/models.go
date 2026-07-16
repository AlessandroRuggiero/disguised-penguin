package models

import "fmt"

type RegistryType string

const (
	RegistryTypeGitHub RegistryType = "github"
)

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
