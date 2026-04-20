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
}

func MakeRegistryType(s string) (RegistryType, error) {
	switch s {
	case "github":
		return RegistryTypeGitHub, nil
	default:
		return "", fmt.Errorf("invalid registry type: %s", s)
	}
}
