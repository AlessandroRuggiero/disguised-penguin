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
