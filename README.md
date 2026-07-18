# Disguised Penguin (dp)

[![CI](https://github.com/AlessandroRuggiero/disguised-penguin/actions/workflows/ci.yml/badge.svg)](https://github.com/AlessandroRuggiero/disguised-penguin/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/AlessandroRuggiero/disguised-penguin?style=flat&logo=github)](https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/AlessandroRuggiero/disguised-penguin?style=flat&logo=go)](go.mod)
[![License](https://img.shields.io/github/license/AlessandroRuggiero/disguised-penguin?style=flat&logo=opensourceinitiative&logoColor=white)](LICENSE)

Run CLI applications in secure, isolated containers (Docker or Podman) without cluttering your system. Disguised Penguin completely sandboxes your tools, preventing them from accessing sensitive files on your host machine, while keeping them as **seamless** to use as native applications.

### Examples:
- **AI CLI Agents (like `opencode`)**: Run AI assistants safely. They get full access to your current project workspace, but are physically blocked from reading your personal `~/.ssh` keys or browsing your private system files.
- **Node.js/NPM Packages (like `firebase`)**: Run tools like `firebase deploy` without polluting your host machine. Global npm packages and their sprawling dependency trees are frequently targets for supply chain attacks; running them through `dp` ensures any malicious scripts remain strictly isolated from your system.

To see practical usage examples, check out the [Usage Examples](#usage-examples) section below.

## Installation

Download the latest `dp` binary from the [GitHub Releases](https://github.com/AlessandroRuggiero/disguised-penguin/releases) page and place it in your PATH (e.g., `/usr/local/bin/dp` or `~/.local/bin/dp`).

```bash
# Linux (amd64)
mkdir -p ~/.local/bin
curl -L -o ~/.local/bin/dp https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest/download/dp
chmod +x ~/.local/bin/dp
```

```bash
# macOS (Apple Silicon / Intel, auto-detected)
mkdir -p ~/.local/bin
arch=$([ "$(uname -m)" = "arm64" ] && echo arm64 || echo amd64)
curl -L -o ~/.local/bin/dp "https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest/download/dp-darwin-$arch"
chmod +x ~/.local/bin/dp
```

```powershell
# Windows (PowerShell)
New-Item -ItemType Directory -Force -Path "$env:LOCALAPPDATA\dp" | Out-Null
Invoke-WebRequest -Uri "https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest/download/dp-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\dp\dp.exe"
# Add $env:LOCALAPPDATA\dp to your PATH (e.g. via System Properties > Environment Variables)
```

Remember to install completions: [see below](#enabling-autocompletion).

### Updating `dp`

If you installed `dp` via the GitHub Releases binary, you can update it to the latest release with:

```bash
dp self-update
```

Note: this overwrites the currently running executable, so it may require write permissions to the install path (e.g. `/usr/local/bin`).

### Building from source

If you prefer to build it yourself:

```bash
git clone https://github.com/AlessandroRuggiero/disguised-penguin.git
cd disguised-penguin
make build
# Then manually move bin/dp to your PATH
# Or add an alias in your .bashrc
echo "alias dp='$(pwd)/bin/dp'" >> ~/.bashrc
source ~/.bashrc
```

### Enabling Autocompletion

To enable terminal autocompletion for `dp` commands and installed containerized CLI names, run:

```bash
dp install-completions
```

This will automatically add the completion script source command to your `~/.bashrc` or `~/.zshrc` (or, on Windows, your PowerShell profile). Restart your shell or open a new terminal for it to take effect.

## Usage

### Install a CLI from the remote repository

```bash
dp install <package-name>
```

This pulls the container image and registers the CLI locally.

### Container runtime (Docker/Podman)

By default, `dp` auto-detects a container runtime (prefers Docker if present, otherwise Podman).

- Use a flag: `dp --runtime=podman install <package-name>`
- Or use an env var: `DP_CONTAINER_RUNTIME=podman dp install <package-name>`

To run an installed CLI with Podman:

```bash
dp --runtime=podman <cli-name> [args...]
```

### Run a CLI in a container

```bash
dp <cli-name> [args...]
```

Your current working directory is mounted as `/workspace` inside the container.

### Add a CLI manually

```bash
# This is not recommended for most users, use install instead to pull from a registry
dp add <name> <container-image>
```

### Remove a CLI

```bash
dp remove <name>
```

### Update a CLI

```bash
dp update <name>
```
Updates the CLI package locally by pulling the latest mapped container image.

```bash
dp update
```
Updates every installed CLI. Failures for individual CLIs are reported but don't stop the rest from updating.

### Update `dp` itself

```bash
dp self-update
```
Downloads the latest `dp` binary from GitHub Releases and replaces the current executable.

### List installed CLIs

```bash
dp list
```
Shows all local CLI configurations and mapped containers.

### Manage Remote Registries

Add, list, or remove registries where `dp` searches for packages to install.

```bash
# Add a remote registry
dp registry add <uri> <type> [priority] [name]

# List remote registries
dp registry list

# Remove a remote registry
dp registry remove <uri>

# Display the CLIs in one or more registries matching a glob pattern
dp registry visit [glob]
```

### Manage Workspaces

Workspaces allow you to isolate the state and configurations of your CLIs by using separate persistent volume environments.

```bash
# Add a new workspace
dp workspace add <name>

# List workspaces
dp workspace list

# Remove a workspace
dp workspace remove <name>
```

### Run a CLI in a specific workspace

You can use the `-w` or `--workspace` flag *before* the CLI name to run a tool in a specific workspace environment (defaults to `default`).

```bash
dp -w <workspace-name> <cli-name> [args...]
# or
dp --workspace <workspace-name> <cli-name> [args...]
```
#### Example: Using Multiple Google Accounts with Gemini
If you have two Google accounts, you can create a separate workspace for the second one and run the CLI agent in the corresponding workspace to use that account's free Gemini tokens.

```bash
# start the CLI in the default workspace (google-account-1)
dp gemini

# create a new workspace for the second google account
dp workspace add google-account-2
# start the CLI in the second workspace (google-account-2)
dp -w google-account-2 gemini
```
### Protect Paths Within a Workspace

Your working directory is mounted read-write into the container by default. Mount protections let you lock down individual paths (relative to the workspace root) so a tool can't tamper with them. Each protection has a mode:

- `ro`: read-only (the tool can read the path but not modify it)
- `rw`: read-write (used to re-open a path that a broader protection covers)
- `h`: hidden (shadowed by an empty mount, so the tool sees nothing there)

Protections are stored per-workspace and applied automatically every time you run a CLI in that workspace.

```bash
# Protect a path in a workspace (PATH:MODE)
dp workspace protection add <workspace> <path>:<mode>

# List protections for a workspace
dp workspace protection list <workspace>

# Remove a protection
dp workspace protection remove <workspace> <path>
```

For example, to keep an AI agent out of your git history and secrets in the `default` workspace:

```bash
dp workspace protection add default .git:ro
dp workspace protection add default .env:h
```

To apply a protection for a single run instead of persisting it, use the `--mp` flag (repeatable) *before* the CLI name, just like `-w`:

```bash
dp --mp .git:ro --mp .env:h <cli-name> [args...]
```
### Database Management

```bash
# Danger: Erases the entire CLI database
dp internal erase-db
```

## Usage Examples

```bash
# Install the 'opencode' AI CLI agent from the default registry
dp install opencode
# Run the 'opencode' CLI in a container
dp opencode
# Install the 'firebase' CLI tool
dp install firebase
# Run 'firebase deploy' in a container, with your current directory mounted as /workspace
dp firebase deploy

# Add a github registry and install a package from it
dp registry add https://raw.githubusercontent.com/YourUsername/YourRepo/main github
# Then you can install packages from that registry
dp install your-package-from-github
# List all registries and their packages
dp registry list
# Visit the newly added registry and see its packages
dp registry visit new-registry-name
```

## How It Works

Disguised Penguin maintains a local SQLite database (`~/.local/share/disguised-penguin/data.db` on Linux/macOS, `%LOCALAPPDATA%\disguised-penguin\data.db` on Windows) storing CLI configurations, registries, and workspaces. On startup, `dp` automatically applies embedded database migrations (and will create/seed the DB on first run, including a `default` registry and a `default` workspace).

Workspace state is stored alongside the database under a `workspaces/<workspace>/` directory (with per-CLI persistent volumes under `.../volumes/<cli>/`).

When you run a CLI, it spawns a container with:

- Your current directory mounted to `/workspace`
- Config volumes mounted at their configured paths
- Port mappings exposed as specified

## Requirements
- Platforms: Linux (amd64), macOS (amd64/arm64), and Windows (amd64)
- To run `dp` (release binary): Docker or Podman (available in `PATH`), on macOS/Windows, Docker Desktop or Podman Desktop/`podman machine`
- To build from source: Go 1.25+

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.
