package container

import (
	"slices"
	"testing"
)

func TestAppCgroupFlags(t *testing.T) {
	want := []string{"--cgroup-parent=app-dp.code.slice", "--annotation", "run.oci.systemd.subgroup="}
	if got := AppCgroupFlags("code"); !slices.Equal(got, want) {
		t.Errorf("plain name: got %v, want %v", got, want)
	}

	// A dash or slash must not create a nested slice or another path.
	want[0] = `--cgroup-parent=app-dp.claude\x2dcode.slice`
	if got := AppCgroupFlags("claude-code"); !slices.Equal(got, want) {
		t.Errorf("name with dash: got %v, want %v", got, want)
	}
	want[0] = `--cgroup-parent=app-dp.a\x2fb.slice`
	if got := AppCgroupFlags("a/b"); !slices.Equal(got, want) {
		t.Errorf("name with slash: got %v, want %v", got, want)
	}
}
