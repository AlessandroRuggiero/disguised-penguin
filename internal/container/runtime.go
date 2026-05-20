package container

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Runtime string

const (
	RuntimeAuto   Runtime = "auto"
	RuntimeDocker Runtime = "docker"
	RuntimePodman Runtime = "podman"
)

// DP_CONTAINER_RUNTIME can be set to "docker" or "podman" to force the runtime.
const EnvContainerRuntime = "DP_CONTAINER_RUNTIME"

var lookPath = exec.LookPath

func ResolveRuntime(requested string) (Runtime, error) {
	req := strings.TrimSpace(strings.ToLower(requested))
	if req == "" {
		req = string(RuntimeAuto)
	}

	fromEnv := false

	if req == string(RuntimeAuto) {
		if env := strings.TrimSpace(strings.ToLower(os.Getenv(EnvContainerRuntime))); env != "" {
			req = env
			fromEnv = true
		}
	}

	switch Runtime(req) {
	case RuntimeDocker, RuntimePodman:
		if _, err := lookPath(req); err != nil {
			return "", fmt.Errorf("%s not found in PATH; install it or set %s=podman/docker", req, EnvContainerRuntime)
		}
		return Runtime(req), nil
	case RuntimeAuto:
		if _, err := lookPath(string(RuntimeDocker)); err == nil {
			return RuntimeDocker, nil
		}
		if _, err := lookPath(string(RuntimePodman)); err == nil {
			return RuntimePodman, nil
		}
		return "", fmt.Errorf("no supported container runtime found (looked for %q and %q in PATH)", RuntimeDocker, RuntimePodman)
	default:
		if fromEnv {
			return "", fmt.Errorf("invalid container runtime %q (from %s; expected: auto, docker, podman)", req, EnvContainerRuntime)
		}
		return "", fmt.Errorf("invalid container runtime %q (expected: auto, docker, podman)", requested)
	}
}
