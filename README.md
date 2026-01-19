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

```bash
make build
make install
```

This will install lav to `~/.local/bin/lav`.

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
lav install <path> <app> <version>
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

**Note:** The folder must contain a `bin/` directory.

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

## Environment Variables

- `LAV_ROOT`: Set this to change the base directory (highest priority)
- `XDG_DATA_HOME`: Data directory following XDG Base Directory specification (`$XDG_DATA_HOME/lav` will be used)
- Default: `~/.local/share/lav`

## License

MIT
