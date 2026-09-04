# lav - Local App Versions

A simple tool to manage local application versions using symbolic links.

## Overview

`lav` manages application versions under `~/.local/share/lav/` using a `current` symbolic link.

## Directory Structure

```
~/.local/share/lav/
├── godot/
│   ├── 4.4.1/
│   ├── 4.5.1/
│   ├── 4.6.0/
│   └── current -> 4.5.1
└── go/
    ├── 1.22.0/
    ├── 1.23.0/
    └── current -> 1.23.0
```

## Installation

lav manages `~/.local/bin` and `~/.local/share` and relies on symbolic links, so
it targets **Linux and macOS** (amd64 and arm64).

### From a release

Download the archive for your platform from the
[releases page](https://github.com/minami110/lav/releases).

```bash
tar xzf lav_v0.1.0_linux_amd64.tar.gz
cd lav_v0.1.0_linux_amd64
```

To check the download against the `checksums.txt` published with it:

```bash
grep lav_v0.1.0_linux_amd64.tar.gz checksums.txt | sha256sum -c
# macOS: grep lav_v0.1.0_darwin_arm64.tar.gz checksums.txt | shasum -a 256 -c
```

On macOS, an archive extracted with Finder carries a quarantine flag and the
binary refuses to run. Clear it, or extract with `tar` in a terminal instead:

```bash
xattr -d com.apple.quarantine ./lav
```

Then let lav manage itself, and make sure `~/.local/bin` is on your `PATH`:

```bash
./lav install ./lav lav v0.1.0
export PATH="$HOME/.local/bin:$PATH"   # add this to your shell profile
```

### From source

```bash
make build
make install
```

This installs lav to `~/.local/bin/lav`. The version is taken from
`git describe`, so a build from a checkout is never mistaken for a release.

## Usage

### Display Help

Show all available commands:
```bash
lav --help
# or
lav -h
# or
lav help
```

Show detailed help for each command:
```bash
lav install --help
lav use --help
lav link --help
lav list --help
lav current --help
```

### Check Version

```bash
lav --version
# or
lav -v
```

### Install Binary

Install a binary into the lav structure and create a symbolic link in `~/.local/bin`:

```bash
lav install <path> <app> <version> [--force]
```

Example (self-install):
```bash
./lav install ./lav lav 0.0.0
```

This creates the following structure:
```
~/.local/share/lav/lav/
├── 0.0.0/
│   └── bin/
│       └── lav
└── current -> 0.0.0

~/.local/bin/lav -> ../share/lav/lav/current/bin/lav
```

The binary is stored and linked under `<app>`, whatever the source file is called.
Release assets that carry the version in their filename therefore stay reachable
under a stable name across versions:

```bash
lav install ~/Downloads/myapp-1.2.0-x86_64.AppImage myapp 1.2.0
# ~/.local/share/lav/myapp/1.2.0/bin/myapp
# ~/.local/bin/myapp -> ../share/lav/myapp/current/bin/myapp
```

If `~/.local/bin/<app>` already exists and is not a symlink lav manages — a
binary installed by hand before adopting lav, for instance — the install stops
before anything is changed and says so. Pass `--force` to replace it:

```bash
lav install ~/Downloads/myapp-1.2.0-x86_64.AppImage myapp 1.2.0 --force
```

### Install Folder

You can install a folder containing a bin/ directory. Symbolic links will be created for all executable files in the folder:

```bash
lav install <folder_path> <app> <version>
```

Example (installing Go):
```bash
lav install ~/Downloads/go1.25.6.linux-amd64/go go 1.25.6
```

This creates the following structure:
```
~/.local/share/lav/go/
├── 1.25.6/
│   ├── bin/
│   │   ├── go
│   │   └── gofmt
│   ├── src/
│   ├── pkg/
│   └── ...
└── current -> 1.25.6

~/.local/bin/go -> ../share/lav/go/current/bin/go
~/.local/bin/gofmt -> ../share/lav/go/current/bin/gofmt
```

**Note:** The folder must contain a `bin/` directory. Links are created for the
executable files in it; anything else there (documentation, data files,
subdirectories) is copied but not linked.

### List Versions

Show all apps:
```bash
lav list
```

Show versions for a specific app:
```bash
lav list godot
```

### Check Current Version

Show current version for all apps:
```bash
lav current
```

Show current version for a specific app:
```bash
lav current godot
```

### Switch Version

```bash
lav use godot 4.6.0
```

Switching also recreates any missing `~/.local/bin` entry for the app. If the
command still cannot be reached afterwards — something else occupies its name,
or the version was installed by an older lav under a different filename — `lav
use` reports it instead of exiting quietly.

### Repair Links

```bash
lav link <app> [--force]
```

Recreates the `~/.local/bin` symlinks for the current version of an app and
removes the ones lav left behind that no longer resolve. With no app name, every
installed app is processed.

A version installed by an older lav kept the source filename (for example
`bin/myapp-1.2.0-x86_64.AppImage`), so nothing resolves through
`current/bin/<app>`. `lav link` repairs such a version by renaming that file to
`bin/<app>`:

```bash
lav link myapp
# myapp: renamed bin/myapp-1.2.0-x86_64.AppImage to bin/myapp
# myapp: linked /home/user/.local/bin/myapp
```

The rename only happens when `~/.local/bin/<app>` is a symlink lav created that
no longer resolves. A package whose command is deliberately named differently
from the app — `ripgrep` shipping `bin/rg` — has no such link and is left
exactly as it is.

A command name can only belong to one app: installing an app that ships a
command another lav app already provides is refused unless you pass `--force`.

## Environment Variables

- `LAV_ROOT`: Set this to change the base directory (highest priority)
- `XDG_DATA_HOME`: Data directory following XDG Base Directory specification (`$XDG_DATA_HOME/lav` will be used)
- Default: `~/.local/share/lav`

## Releasing

Pushing a version tag builds the binaries and publishes a GitHub Release with
the archives and a `checksums.txt`.

```bash
git tag v0.1.0
git push origin v0.1.0
```

- The tag must be `vMAJOR.MINOR.PATCH`, optionally with a `-suffix`. Anything
  else is rejected by the workflow, and a tag carrying a suffix (`v0.2.0-rc.1`)
  is published as a prerelease.
- Tag a commit that **contains `.github/workflows/release.yml`**. A tag on an
  older commit starts nothing at all, with no error anywhere.
- The version string is the tag itself, so the release above is installed as
  `lav install ./lav lav v0.1.0`.
- Tag a commit whose tests pass. The workflow runs them again and publishes
  nothing if they fail, but by then the tag already exists.

To redo a release, remove it together with its tag and push the tag again:

```bash
gh release delete v0.1.0 --cleanup-tag --yes
```

Re-running the workflow on a tag that already has a release replaces its
assets rather than failing.

## License

MIT
