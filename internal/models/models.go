package models

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
}
