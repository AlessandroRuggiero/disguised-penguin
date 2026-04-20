package remote

import (
	"disguised-penguin/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
)

func GetRemotePackages(remoteRepoURL string) (map[string]models.RemotePackage, error) {
	resp, err := http.Get(remoteRepoURL + "/pkgs.json")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote packages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pkgs map[string]models.RemotePackage
	if err := json.NewDecoder(resp.Body).Decode(&pkgs); err != nil {
		return nil, fmt.Errorf("failed to decode remote packages: %w", err)
	}
	return pkgs, nil
}
