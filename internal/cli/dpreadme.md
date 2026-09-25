# .dp: this project's dp configuration

This folder is read by `dp` on the host whenever a CLI is run from this
directory. It was created by `dp local variant extend`. dp writes this README
once and never overwrites it, so feel free to edit it.

## Files

- `variants.json`: the CLIs this project extends, each with its build file.
- `build/<cli>.Dockerfile`: the recipe for a variant, starting `FROM` the
  installed CLI's image.
- `mounts.json` (optional): extra overlays on `/workspace` for every run.

## Extending a CLI

1. `dp local variant extend <cli>` declares the variant and creates
   `build/<cli>.Dockerfile`.
2. Edit that Dockerfile to add what the project needs (packages, tools, config).
3. `dp local variant build <cli>` builds it. Rebuild after every change to the
   build files.
4. `dp <cli>` run from this directory now uses the variant image.

The build context is the **project root**, not `.dp/`, so `COPY` paths start
from the root: `COPY .dp/build/setup.sh /tmp/setup.sh`.

The base image is pulled on every build, so a rebuild also picks up updates to
the CLI.

## Tips

- **Check the base OS first.** CLIs are not all built on the same distro (many
  are Debian, some are Fedora or Alpine). `extend` writes a `# Base OS:` line
  at the top of the Dockerfile with the distro and its package manager, so use
  that one (`apt-get`, `dnf`, `apk`, ...) rather than assuming. If the line is
  missing, check with
  `podman run --rm --entrypoint cat <image> /etc/os-release`.
- **Many install steps? Use a script.** Instead of a long chain of `RUN`
  lines, put them in `build/setup.sh` and run it once:

  ```dockerfile
  COPY .dp/build/setup.sh /tmp/setup.sh
  RUN sh /tmp/setup.sh && rm /tmp/setup.sh
  ```

  One script can also be shared by the Dockerfiles of several variants.
- If the base image runs as a non-root user, switch with `USER root` before
  installing and back to the original user afterwards (check it with
  `podman image inspect` or `docker image inspect`).
- Each variant image is tagged for this directory only, so it never affects
  other projects or the installed CLI.

## mounts.json

Overlays paths in `/workspace` for every CLI run from this directory:

```json
{
  "mounts": [
    { "source": "secrets", "path": "config/secrets", "mode": "ro" },
    { "path": "node_modules", "mode": "h" }
  ]
}
```

`mode` is `ro` (read-only), `rw` (read-write) or `h` (hidden). `source` is an
optional folder under `.dp/` to show at `path`. It is then hidden at its
original place, and it can't be combined with `h`.

## For AI agents

- Inside a dp container, `/workspace/.dp` is mounted **read-only**, so a tool
  cannot change the recipes that govern its own future runs. To change
  anything here, propose the edit and let the user apply it on the host, then
  rebuild with `dp local variant build <cli>`.
- dp itself only reads the files above. Anything else here is used only if a
  build file `COPY`s it or `mounts.json` names it as a `source`.
