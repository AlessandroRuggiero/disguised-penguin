# Disguised Penguin (dp)

Run CLI applications in secure, isolated Docker containers without cluttering your system. Disguised Penguin completely sandboxes your tools, preventing them from accessing sensitive files on your host machine, while keeping them as **seamless** to use as native applications.

### Examples:
- **AI CLI Agents (like `opencode`)**: Run AI assistants safely. They get full access to your current project workspace, but are physically blocked from reading your personal `~/.ssh` keys or browsing your private system files.
- **Node.js/NPM Packages (like `firebase`)**: Run tools like `firebase deploy` without polluting your host machine. Global npm packages and their sprawling dependency trees are frequently targets for supply chain attacks; running them through `dp` ensures any malicious scripts remain strictly isolated from your system.

To see practical usage examples, check out the [Usage Examples](#usage-examples) section below.

## Installation

Download the latest `dp` binary from the [GitHub Releases](https://github.com/AlessandroRuggiero/disguised-penguin/releases) page and place it in your PATH (e.g., `/usr/local/bin/dp`).

```bash
# Example for Linux/macOS
curl -L -o /usr/local/bin/dp https://github.com/AlessandroRuggiero/disguised-penguin/releases/latest/download/dp
chmod +x /usr/local/bin/dp
```

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

This will automatically add the completion script source command to your `~/.bashrc` or `~/.zshrc`. Restart your shell or open a new terminal for it to take effect.

## Usage

### Install a CLI from the remote repository

```bash
dp install <package-name>
```

This pulls the Docker image and registers the CLI locally.

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

### Database Management

```bash
# Danger: Erases the entire CLI database
dp erase-db
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

Disguised Penguin maintains a local SQLite database (`~/.local/share/disguised-penguin/data.db`) storing CLI configurations. When you run a CLI, it spawns a Docker container with:

- Your current directory mounted to `/workspace`
- Config volumes mounted at their configured paths
- Port mappings exposed as specified

## Requirements

- Go 1.25+
- Docker

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.
