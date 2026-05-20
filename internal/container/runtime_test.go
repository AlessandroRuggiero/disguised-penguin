package container

import (
	"errors"
	"strings"
	"testing"
)

func withLookPath(t *testing.T, f func(string) (string, error)) func() {
	prev := lookPath
	lookPath = f
	return func() { lookPath = prev }
}

func TestResolveRuntime_AutoPrefersDockerThenPodman(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "")
	defer withLookPath(t, func(name string) (string, error) {
		switch name {
		case "docker":
			return "/usr/bin/docker", nil
		case "podman":
			return "/usr/bin/podman", nil
		default:
			return "", errors.New("not found")
		}
	})()

	r, err := ResolveRuntime("auto")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if r != RuntimeDocker {
		t.Fatalf("expected %q, got %q", RuntimeDocker, r)
	}
}

func TestResolveRuntime_AutoFallsBackToPodman(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "")
	defer withLookPath(t, func(name string) (string, error) {
		if name == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	})()

	r, err := ResolveRuntime("auto")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if r != RuntimePodman {
		t.Fatalf("expected %q, got %q", RuntimePodman, r)
	}
}

func TestResolveRuntime_EnvOverridesAuto(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "podman")
	defer withLookPath(t, func(name string) (string, error) {
		if name == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	})()

	r, err := ResolveRuntime("auto")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if r != RuntimePodman {
		t.Fatalf("expected %q, got %q", RuntimePodman, r)
	}
}

func TestResolveRuntime_ExplicitRequiresBinary(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "")
	defer withLookPath(t, func(name string) (string, error) {
		return "", errors.New("not found")
	})()

	if _, err := ResolveRuntime("docker"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolveRuntime_InvalidValue(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "")
	defer withLookPath(t, func(name string) (string, error) {
		return "", errors.New("not found")
	})()

	if _, err := ResolveRuntime("containerd"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolveRuntime_AutoWithInvalidEnvValue(t *testing.T) {
	t.Setenv(EnvContainerRuntime, "containerd")
	defer withLookPath(t, func(name string) (string, error) {
		return "", errors.New("not found")
	})()

	_, err := ResolveRuntime("auto")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "containerd") {
		t.Fatalf("expected error to mention invalid env value; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), EnvContainerRuntime) {
		t.Fatalf("expected error to mention %s; got %q", EnvContainerRuntime, err.Error())
	}
}
