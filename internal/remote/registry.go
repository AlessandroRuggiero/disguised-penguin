package remote

import (
	"disguised-penguin/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type remoteFile uint8

const (
	pkgsFile remoteFile = iota
	infoFile
)

func getGitHubFile(uri string, file remoteFile) ([]byte, error) {
	var fileName string
	switch file {
	case pkgsFile:
		fileName = "pkgs.json"
	case infoFile:
		fileName = "info.json"
	default:
		return nil, fmt.Errorf("unknown remote file type: %d", file)
	}

	resp, err := http.Get(uri + "/" + fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func getRemoteFile(remoteRepo models.RemoteRegistry, file remoteFile) ([]byte, error) {
	switch remoteRepo.RegistryType {
	case models.RegistryTypeGitHub:
		return getGitHubFile(remoteRepo.URI, file)
	default:
		return nil, fmt.Errorf("unsupported registry type: %s", remoteRepo.RegistryType)
	}
}

func GetRemotePackages(remoteRepo models.RemoteRegistry) (map[string]models.RemotePackage, error) {
	resp, err := getRemoteFile(remoteRepo, pkgsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote packages: %w", err)
	}

	var pkgs map[string]models.RemotePackage
	err = json.Unmarshal(resp, &pkgs)
	if err != nil {
		return nil, fmt.Errorf("failed to decode remote packages: %w", err)
	}
	return pkgs, nil
}

func GetRemoteInfo(remoteRepo models.RemoteRegistry) (models.RemotePackageInfo, error) {
	resp, err := getRemoteFile(remoteRepo, infoFile)
	if err != nil {
		return models.RemotePackageInfo{}, fmt.Errorf("failed to fetch remote package info: %w", err)
	}

	var info models.RemotePackageInfo
	err = json.Unmarshal(resp, &info)
	if err != nil {
		return models.RemotePackageInfo{}, fmt.Errorf("failed to decode remote package info: %w", err)
	}
	return info, nil
}
