package models

import (
	"encoding/json"
	"testing"
)

func TestConfigMountUnmarshal(t *testing.T) {
	var mounts map[string]ConfigMount
	in := `{
		"config": "/root/.config/tool",
		"cache": {"path": "/root/.cache", "type": "dir"},
		"netrc": {"path": "/root/.netrc", "type": "file"}
	}`
	if err := json.Unmarshal([]byte(in), &mounts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// A bare string stays a directory (backward compatible), normalized to "dir".
	if m := mounts["config"]; m.Path != "/root/.config/tool" || m.Type != "dir" || m.IsFile() {
		t.Errorf("config: got %+v", m)
	}
	if m := mounts["cache"]; m.Path != "/root/.cache" || m.IsFile() {
		t.Errorf("cache: got %+v", m)
	}
	if m := mounts["netrc"]; m.Path != "/root/.netrc" || !m.IsFile() {
		t.Errorf("netrc: got %+v", m)
	}
}

func TestConfigMountRoundTrip(t *testing.T) {
	// Marshaling always emits the object form; unmarshaling recovers it.
	for _, want := range []ConfigMount{
		{Path: "/root/.config", Type: "dir"},
		{Path: "/root/.netrc", Type: "file"},
	} {
		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal %+v: %v", want, err)
		}
		var got ConfigMount
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got != want {
			t.Errorf("round trip: got %+v, want %+v (via %s)", got, want, data)
		}
	}
}

func TestConfigMountUnmarshalErrors(t *testing.T) {
	for _, in := range []string{
		`{"type": "file"}`,                 // missing path
		`{"path": "/x", "type": "socket"}`, // invalid type
	} {
		var m ConfigMount
		if err := json.Unmarshal([]byte(in), &m); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}

func TestExtraRunArgUnmarshal(t *testing.T) {
	in := `{
		"container": "ghcr.io/foo/bar:latest",
		"extra_run_args": [
			{"args": ["--cap-add", "SYS_PTRACE"], "description": "attach to processes"},
			{"args": ["--network=host"], "description": "reach local services"}
		]
	}`
	var pkg RemotePackage
	if err := json.Unmarshal([]byte(in), &pkg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pkg.ExtraRunArgs) != 2 {
		t.Fatalf("expected 2 extra run args, got %d", len(pkg.ExtraRunArgs))
	}
	if got, want := pkg.ExtraRunArgs[0].String(), "--cap-add SYS_PTRACE"; got != want {
		t.Errorf("String(): got %q, want %q", got, want)
	}
	if got, want := pkg.ExtraRunArgs[1].Description, "reach local services"; got != want {
		t.Errorf("description: got %q, want %q", got, want)
	}
}

func TestExtraRunArgUnmarshalErrors(t *testing.T) {
	for name, in := range map[string]string{
		"missing description": `{"args": ["--privileged"]}`,
		"blank description":   `{"args": ["--privileged"], "description": "   "}`,
		"missing args":        `{"description": "why"}`,
		"empty args":          `{"args": [], "description": "why"}`,
		"empty token":         `{"args": ["--cap-add", ""], "description": "why"}`,
		// A leading positional would be read by the runtime as the image name.
		"positional first": `{"args": ["ubuntu"], "description": "why"}`,
		"not an object":    `"--privileged"`,
	} {
		var a ExtraRunArg
		if err := json.Unmarshal([]byte(in), &a); err == nil {
			t.Errorf("%s: expected error for %s", name, in)
		}
	}
}

func TestExtraRunArgsFlat(t *testing.T) {
	got := ExtraRunArgsFlat([]ExtraRunArg{
		{Args: []string{"--cap-add", "SYS_PTRACE"}},
		{Args: []string{"--network=host"}},
	})
	want := []string{"--cap-add", "SYS_PTRACE", "--network=host"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if flat := ExtraRunArgsFlat(nil); len(flat) != 0 {
		t.Errorf("expected no tokens for nil args, got %v", flat)
	}
}
