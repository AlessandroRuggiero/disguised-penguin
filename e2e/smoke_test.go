//go:build smoke

// Smoke suite: drives every `dp` command that needs neither a container runtime
// nor network access, asserting exit codes and output. Each test isolates its
// state to a temp XDG_DATA_HOME (see helpers_test.go), so it never touches the
// developer's real database or workspaces.
//
// Run with:  go test -tags=smoke ./e2e/...
package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	data := t.TempDir()

	// dp --version
	out, code := run(t, data, "--version")
	mustOK(t, "dp version", out, code)

	// dp -v
	out, code = run(t, data, "-v")
	mustOK(t, "dp version", out, code)
}

func TestHelp(t *testing.T) {
	data := t.TempDir()

	// dp --help
	out, code := run(t, data, "--help")
	mustOK(t, "Usage", out, code)

	// dp -h
	out, code = run(t, data, "-h")
	mustOK(t, "Usage", out, code)
}

func TestNoArgs(t *testing.T) {
	data := t.TempDir()

	// dp
	out, code := run(t, data)
	mustFail(t, "", out, code)
}

func TestCLIAddListRemove(t *testing.T) {
	data := t.TempDir()

	// dp list
	out, code := run(t, data, "list")
	mustOK(t, "Available CLIs:", out, code)

	// dp add mytool docker.io/library/hello:latest
	out, code = run(t, data, "add", "mytool", "docker.io/library/hello:latest")
	mustOK(t, "Successfully added CLI 'mytool'", out, code)

	// dp list
	out, code = run(t, data, "list")
	mustOK(t, "mytool", out, code)

	// dp remove mytool
	out, code = run(t, data, "remove", "mytool")
	mustOK(t, "Successfully removed CLI 'mytool'", out, code)

	// dp list
	out, code = run(t, data, "list")
	if strings.Contains(out, "mytool") {
		t.Fatalf("removed CLI still listed:\n%s", out)
	}
}

func TestCLIAddErrors(t *testing.T) {
	data := t.TempDir()

	// dp add onlyonearg
	out, code := run(t, data, "add", "onlyonearg")
	mustFail(t, "", out, code)

	// dp add dup docker.io/library/hello:latest
	out, code = run(t, data, "add", "dup", "docker.io/library/hello:latest")
	mustOK(t, "Successfully added CLI 'dup'", out, code)

	// dp add dup docker.io/library/hello:latest
	out, code = run(t, data, "add", "dup", "docker.io/library/hello:latest")
	mustFail(t, "", out, code)
}

func TestCLIRemoveSharedImage(t *testing.T) {
	data := t.TempDir()

	// dp add a docker.io/library/shared:latest
	out, code := run(t, data, "add", "a", "docker.io/library/shared:latest")
	mustOK(t, "Successfully added CLI 'a'", out, code)

	// dp add b docker.io/library/shared:latest
	out, code = run(t, data, "add", "b", "docker.io/library/shared:latest")
	mustOK(t, "Successfully added CLI 'b'", out, code)

	// dp remove a  (image still used by b, so no runtime is invoked)
	out, code = run(t, data, "remove", "a")
	mustOK(t, "still used by another CLI", out, code)
}

func TestCLIRemoveMissing(t *testing.T) {
	data := t.TempDir()

	// dp remove ghost
	out, code := run(t, data, "remove", "ghost")
	mustOK(t, "No CLI found with name 'ghost'", out, code)
}

func TestWorkspaceAddListRemove(t *testing.T) {
	data := t.TempDir()

	// dp workspace list
	out, code := run(t, data, "workspace", "list")
	mustOK(t, "default", out, code)

	// dp workspace add proj
	out, code = run(t, data, "workspace", "add", "proj")
	mustOK(t, "Successfully added workspace 'proj'", out, code)

	// dp workspace list
	out, code = run(t, data, "workspace", "list")
	mustOK(t, "proj", out, code)

	// dp workspace remove proj
	out, code = run(t, data, "workspace", "remove", "proj")
	mustOK(t, "Successfully removed workspace 'proj'", out, code)
}

func TestWorkspaceErrors(t *testing.T) {
	data := t.TempDir()

	// dp workspace add dev
	out, code := run(t, data, "workspace", "add", "dev")
	mustOK(t, "Successfully added workspace 'dev'", out, code)

	// dp workspace add dev
	out, code = run(t, data, "workspace", "add", "dev")
	mustFail(t, "already exists", out, code)

	// dp workspace remove default
	out, code = run(t, data, "workspace", "remove", "default")
	mustFail(t, "cannot remove the 'default' workspace", out, code)

	// dp workspace remove ghost
	out, code = run(t, data, "workspace", "remove", "ghost")
	mustOK(t, "No workspace found with name 'ghost'", out, code)
}

func TestWorkspaceClean(t *testing.T) {
	data := t.TempDir()

	// dp workspace clean default mytool
	out, code := run(t, data, "workspace", "clean", "default", "mytool")
	mustOK(t, "No data found for CLI 'mytool'", out, code)

	// dp workspace clean ghost mytool
	out, code = run(t, data, "workspace", "clean", "ghost", "mytool")
	mustFail(t, "workspace 'ghost' not found", out, code)
}

func TestWorkspaceProtectionAddModes(t *testing.T) {
	data := t.TempDir()

	// dp workspace protection list default
	out, code := run(t, data, "workspace", "protection", "list", "default")
	mustOK(t, "No protections set for workspace 'default'", out, code)

	// dp workspace protection add default .git:ro
	out, code = run(t, data, "workspace", "protection", "add", "default", ".git:ro")
	mustOK(t, "Protected '.git' as 'ro' in workspace 'default'", out, code)

	// dp workspace protection add default src/config:rw
	out, code = run(t, data, "workspace", "protection", "add", "default", "src/config:rw")
	mustOK(t, "Protected 'src/config' as 'rw' in workspace 'default'", out, code)

	// dp workspace protection add default .env:h
	out, code = run(t, data, "workspace", "protection", "add", "default", ".env:h")
	mustOK(t, "Protected '.env' as 'h' in workspace 'default'", out, code)

	// dp workspace protection list default
	out, code = run(t, data, "workspace", "protection", "list", "default")
	mustOK(t, ".git -> ro", out, code)
	mustOK(t, "src/config -> rw", out, code)
	mustOK(t, ".env -> h", out, code)
}

func TestWorkspaceProtectionReAddUpdatesMode(t *testing.T) {
	data := t.TempDir()

	// dp workspace protection add default .git:ro
	out, code := run(t, data, "workspace", "protection", "add", "default", ".git:ro")
	mustOK(t, "Protected '.git' as 'ro' in workspace 'default'", out, code)

	// dp workspace protection add default .git:rw
	out, code = run(t, data, "workspace", "protection", "add", "default", ".git:rw")
	mustOK(t, "Protected '.git' as 'rw' in workspace 'default'", out, code)

	// dp workspace protection list default
	out, code = run(t, data, "workspace", "protection", "list", "default")
	mustOK(t, ".git -> rw", out, code)
	if strings.Contains(out, ".git -> ro") {
		t.Fatalf("re-add should replace the mode, not duplicate it:\n%s", out)
	}
}

func TestWorkspaceProtectionRemove(t *testing.T) {
	data := t.TempDir()

	// dp workspace protection add default .git:ro
	out, code := run(t, data, "workspace", "protection", "add", "default", ".git:ro")
	mustOK(t, "Protected '.git' as 'ro' in workspace 'default'", out, code)

	// dp workspace protection remove default ./.git  (path is normalized to match)
	out, code = run(t, data, "workspace", "protection", "remove", "default", "./.git")
	mustOK(t, "Removed protection for '.git' in workspace 'default'", out, code)

	// dp workspace protection list default
	out, code = run(t, data, "workspace", "protection", "list", "default")
	mustOK(t, "No protections set for workspace 'default'", out, code)

	// dp workspace protection remove default .git
	out, code = run(t, data, "workspace", "protection", "remove", "default", ".git")
	mustOK(t, "No protection found for '.git' in workspace 'default'", out, code)
}

func TestWorkspaceProtectionAlias(t *testing.T) {
	data := t.TempDir()

	// dp ws prot ls default
	out, code := run(t, data, "ws", "prot", "ls", "default")
	mustOK(t, "No protections set for workspace 'default'", out, code)
}

func TestWorkspaceProtectionErrors(t *testing.T) {
	data := t.TempDir()

	// dp workspace protection add default .git:xx
	out, code := run(t, data, "workspace", "protection", "add", "default", ".git:xx")
	mustFail(t, "", out, code)

	// dp workspace protection add default /etc/passwd:ro
	out, code = run(t, data, "workspace", "protection", "add", "default", "/etc/passwd:ro")
	mustFail(t, "", out, code)

	// dp workspace protection add ghost .git:ro
	out, code = run(t, data, "workspace", "protection", "add", "ghost", ".git:ro")
	mustFail(t, "workspace 'ghost' not found", out, code)

	// dp workspace protection remove ghost .git
	out, code = run(t, data, "workspace", "protection", "remove", "ghost", ".git")
	mustFail(t, "workspace 'ghost' not found", out, code)

	// dp workspace protection list ghost
	out, code = run(t, data, "workspace", "protection", "list", "ghost")
	mustFail(t, "workspace 'ghost' not found", out, code)
}

func TestRegistryListRemove(t *testing.T) {
	data := t.TempDir()

	// dp registry list
	out, code := run(t, data, "registry", "list")
	mustOK(t, "default", out, code)

	// dp registry remove bogus-uri
	out, code = run(t, data, "registry", "remove", "bogus-uri")
	mustOK(t, "No registry found with URI 'bogus-uri'", out, code)

	// dp registry remove <default URI>
	out, code = run(t, data, "registry", "remove", defaultRegistryURI)
	mustOK(t, "Successfully removed registry", out, code)

	// dp registry list
	out, code = run(t, data, "registry", "list")
	if strings.Contains(out, defaultRegistryURI) {
		t.Fatalf("removed registry still listed:\n%s", out)
	}
}

func TestRunUnknownCLI(t *testing.T) {
	data := t.TempDir()

	// Drop the seeded registry first so the "did you mean" lookup stays offline.
	// dp registry remove <default URI>
	out, code := run(t, data, "registry", "remove", defaultRegistryURI)
	mustOK(t, "Successfully removed registry", out, code)

	// dp definitely-not-installed
	out, code = run(t, data, "definitely-not-installed")
	mustFail(t, "is not installed", out, code)
}

func TestRunFlagErrors(t *testing.T) {
	data := t.TempDir()

	// dp -w
	out, code := run(t, data, "-w")
	mustFail(t, "flag needs an argument", out, code)

	// dp add mytool docker.io/library/hello:latest
	out, code = run(t, data, "add", "mytool", "docker.io/library/hello:latest")
	mustOK(t, "Successfully added CLI 'mytool'", out, code)

	// dp -w ghost mytool  (errors on the missing workspace before touching a runtime)
	out, code = run(t, data, "-w", "ghost", "mytool")
	mustFail(t, "workspace 'ghost' not found", out, code)
}

func TestEraseDB(t *testing.T) {
	data := t.TempDir()
	dbFile := filepath.Join(data, "disguised-penguin", "data.db")

	// echo n | dp internal erase-db
	out, code := runInput(t, data, "n\n", "internal", "erase-db")
	mustOK(t, "Aborting", out, code)
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("db should survive an aborted erase: %v", err)
	}

	// echo y | dp internal erase-db
	out, code = runInput(t, data, "y\n", "internal", "erase-db")
	mustOK(t, "Successfully erased", out, code)
	if _, err := os.Stat(dbFile); !os.IsNotExist(err) {
		t.Fatalf("db should be gone after confirmed erase; stat err = %v", err)
	}
}

// localRegistry serves a pkgs.json/info.json pair over loopback so install can
// be exercised without reaching the real registry. The "github" registry type
// is just an HTTP GET of "<uri>/<file>", so a plain test server is enough.
func localRegistry(t *testing.T, pkgsJSON string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/info.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"default_name": "test-registry", "description": "smoke test registry"}`)
	})
	mux.HandleFunc("/pkgs.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, pkgsJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// A package asking for extra runtime args must not be installed silently when
// there is nobody to accept them.
func TestInstallExtraRunArgsRefusedWithoutAnswer(t *testing.T) {
	data := t.TempDir()
	uri := localRegistry(t, `{
		"risky": {
			"container": "docker.io/library/hello:latest",
			"extra_run_args": [
				{"args": ["--cap-add", "SYS_PTRACE"], "description": "attach to processes"}
			]
		}
	}`)

	// dp registry remove <default URI>   (keep the lookup offline)
	out, code := run(t, data, "registry", "remove", defaultRegistryURI)
	mustOK(t, "Successfully removed registry", out, code)

	// dp registry add <local uri> github 10 local
	out, code = run(t, data, "registry", "add", uri, "github", "10", "local")
	mustOK(t, "Successfully added registry", out, code)

	// dp install risky   (nothing on stdin, as in a script or CI)
	out, code = run(t, data, "install", "risky")
	mustFail(t, "Re-run with --yes", out, code)
	// The flags themselves are still shown, so the log says what was refused.
	if !strings.Contains(out, "--cap-add SYS_PTRACE") {
		t.Fatalf("output should list the requested args; got:\n%s", out)
	}
	if !strings.Contains(out, "attach to processes") {
		t.Fatalf("output should list the arg descriptions; got:\n%s", out)
	}

	// dp list   (nothing was installed, and the image was never pulled)
	out, _ = run(t, data, "list")
	if strings.Contains(out, "risky") {
		t.Fatalf("refused package should not be installed:\n%s", out)
	}
}

// A malformed manifest is rejected at fetch time rather than producing a CLI
// that fails later at container start.
func TestInstallExtraRunArgsMissingDescription(t *testing.T) {
	data := t.TempDir()
	uri := localRegistry(t, `{
		"sloppy": {
			"container": "docker.io/library/hello:latest",
			"extra_run_args": [{"args": ["--privileged"]}]
		}
	}`)

	// dp registry remove <default URI>
	out, code := run(t, data, "registry", "remove", defaultRegistryURI)
	mustOK(t, "Successfully removed registry", out, code)

	// dp registry add <local uri> github 10 local
	out, code = run(t, data, "registry", "add", uri, "github", "10", "local")
	mustOK(t, "Successfully added registry", out, code)

	// dp install sloppy
	out, code = run(t, data, "install", "sloppy")
	mustFail(t, "not found in any remote registry", out, code)
	if !strings.Contains(out, "missing \"description\"") {
		t.Fatalf("output should explain the manifest problem; got:\n%s", out)
	}
}

func TestSettingsGetSet(t *testing.T) {
	data := t.TempDir()

	// dp settings get alias_mode
	out, code := run(t, data, "settings", "get", "alias_mode")
	mustOK(t, "none (0)", out, code)

	// dp settings set alias_mode all
	out, code = run(t, data, "settings", "set", "alias_mode", "all")
	mustOK(t, "Set alias_mode to all (2)", out, code)

	// dp settings set alias_mode 1
	out, code = run(t, data, "settings", "set", "alias_mode", "1")
	mustOK(t, "Set alias_mode to some (1)", out, code)

	// dp settings list
	out, code = run(t, data, "settings", "list")
	mustOK(t, "alias_mode = some (1)", out, code)

	// dp settings set alias_mode 9
	out, code = run(t, data, "settings", "set", "alias_mode", "9")
	mustFail(t, "invalid value", out, code)

	// dp settings get nope
	out, code = run(t, data, "settings", "get", "nope")
	mustFail(t, "unknown setting", out, code)
}

func TestAliasesInCompletion(t *testing.T) {
	data := t.TempDir()
	run(t, data, "add", "foo", "img-foo")
	run(t, data, "add", "bar", "img-bar")

	// alias_mode defaults to none: plain completion script, no aliases.
	out, code := run(t, data, "completion", "bash")
	mustOK(t, "bash completion V2 for dp", out, code)
	if strings.Contains(out, "alias ") {
		t.Fatalf("unexpected aliases with alias_mode none:\n%s", out)
	}

	// some: only CLIs enabled with dp alias add
	run(t, data, "settings", "set", "alias_mode", "some")
	out, code = run(t, data, "alias", "add", "foo")
	mustOK(t, "Enabled alias for 'foo'", out, code)
	out, code = run(t, data, "completion", "bash")
	mustOK(t, "alias foo='dp foo'", out, code)
	if strings.Contains(out, "alias bar=") {
		t.Fatalf("bar should not be aliased in some mode:\n%s", out)
	}
	out, code = run(t, data, "alias", "list")
	mustOK(t, "- foo", out, code)

	// all: every CLI, for every shell
	run(t, data, "settings", "set", "alias_mode", "all")
	out, code = run(t, data, "completion", "zsh")
	mustOK(t, "alias bar='dp bar'", out, code)
	mustOK(t, "#compdef dp", out, code)
	out, code = run(t, data, "completion", "powershell")
	mustOK(t, "function foo { dp foo @args }", out, code)
	out, code = run(t, data, "completion", "fish")
	mustOK(t, "alias bar 'dp bar'", out, code)

	// dp alias add missing
	out, code = run(t, data, "alias", "add", "missing")
	mustFail(t, "not installed", out, code)
}

func TestVariantsListAndPruneWithoutRuntime(t *testing.T) {
	data := t.TempDir()

	// dp variants list
	out, code := run(t, data, "variants", "list")
	mustOK(t, "No tracked variants", out, code)

	// dp variants prune (cutoff is required)
	out, code = run(t, data, "variants", "prune")
	mustFail(t, "older-than", out, code)

	// dp variants prune --older-than soon
	out, code = run(t, data, "variants", "prune", "--older-than", "soon")
	mustFail(t, "invalid age", out, code)

	// dp variants prune --older-than 30d (nothing tracked)
	out, code = run(t, data, "variants", "prune", "--older-than", "30d")
	mustOK(t, "Nothing to prune.", out, code)

	// dp update --help lists the new flag
	out, code = run(t, data, "update", "--help")
	mustOK(t, "--variants", out, code)
}
