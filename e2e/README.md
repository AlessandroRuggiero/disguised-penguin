# e2e tests

End-to-end tests that build the real `dp` binary and drive it as a user would.
Each test runs against an isolated `XDG_DATA_HOME` (a temp dir), so it never
touches your real database or workspaces.

Two suites, selected by build tag:

- **smoke** (`-tags=smoke`) — every command that needs no container runtime and
  no network. Hermetic, fast, runs on all platforms.
- **integration** (`-tags=integration`) — launches real containers through `dp`
  and checks mount-protection enforcement. Needs docker/podman, pulls `busybox`,
  and drives `dp` under a pseudo-terminal (it hardcodes `docker run -it`).

## Running

```sh
make test-smoke        # hermetic suite
make test-integration  # needs docker/podman
make test-e2e          # both
```

The integration suite needs `github.com/creack/pty` (test-only dependency, never
compiled into the shipped binary).

## AI involvement

This `e2e/` directory was written with assistance from an AI coding tool
(Claude Code). The rest of the project is human-authored.
