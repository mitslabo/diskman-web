# diskman-web

A web-based disk management UI for copying and erasing drives in multi-bay enclosures.

## Features

- Visual slot grid for each enclosure — click to select source / destination
- **COPY**: 3-pass recovery copy using `ddrescue`
- **ERASE**: zero-fill using `dd`
- Real-time progress via SSE (percent, rate, remaining time)
- Multiple enclosure profiles in one config file
- Active enclosure selectable via CLI flag, config file, or first-run browser popup
- Job history with cancel support

## Requirements

- Linux (uses `ddrescue`, `dd`, SIGUSR1)
- [`ddrescue`](https://www.gnu.org/software/ddrescue/) for copy operations
- `dd` (GNU coreutils) for erase operations

## Installation

Download the prebuilt binary from the GitHub Releases page and use it directly.

1. Download the binary for your architecture from Releases (for example: `diskman-web.amd64`, `diskman-web.arm64`, `diskman-web.arm`).
2. Place it in any directory (for example: `/usr/local/bin` or your project directory).
3. Make it executable:

```sh
chmod +x ./diskman-web.amd64
```

4. Run it:

```sh
./diskman-web.amd64
```

## Usage

```
./diskman-web [flags]

Flags:
  -config    <path>   Path to config file (default: platform config dir)
  -enclosure <name>   Active enclosure name (overrides config activeEnclosure)
  -addr      <addr>   Listen address, e.g. :8080 (overrides config)
  -daemon             Run as background daemon process (detach from terminal)
```

On first run, a default config file is created at:
- Linux/macOS: `~/.config/diskman-web/config.json`
- Windows: `%AppData%\diskman-web\config.json`

Then open `http://localhost:8080` in a browser.

## Configuration

```json
{
  "addr": ":8080",
  "logFile": "~/.local/share/diskman-web/jobs.jsonl",
  "mapDir": "~/.local/share/diskman-web/maps",
  "activeEnclosure": "2-bay",
  "enclosures": [
    {
      "name": "2-bay",
      "rows": 2,
      "cols": 1,
      "grid": [[1], [2]],
      "devices": {
        "1": "/dev/sda",
        "2": "/dev/sdb"
      }
    },
    {
      "name": "6-bay",
      "rows": 6,
      "cols": 1,
      "grid": [[1], [2], [3], [4], [5], [6]],
      "devices": {
        "1": "/dev/disk1",
        "2": "/dev/disk2",
        "3": "/dev/disk3",
        "4": "/dev/disk4",
        "5": "/dev/disk5",
        "6": "/dev/disk6"
      }
    }
  ]
}
```

| Field             | Description                                                  |
|-------------------|--------------------------------------------------------------|
| `addr`            | HTTP listen address                                          |
| `logFile`         | Path to job history file (JSONL)                             |
| `mapDir`          | Directory for ddrescue map files                             |
| `activeEnclosure` | Name of the enclosure to use on startup                      |
| `enclosures`      | Array of enclosure definitions                               |
| `enclosure.grid`  | 2D array of slot numbers matching the physical bay layout    |
| `enclosure.devices` | Map of slot number (string) → block device path            |

If `activeEnclosure` is not set and `--enclosure` is not given, a selection popup is shown in the browser on first load and the choice is saved to the config file. After that initial setup, the web UI no longer offers enclosure switching.

## Enclosure Selection Priority

1. `--enclosure` CLI flag
2. `activeEnclosure` in config file
3. Browser popup (saves selection back to config)

## Build From Source

Use this only when you need to build binaries yourself.

### Install task runner

If you do not have `task` installed yet:

```sh
go install github.com/go-task/task/v3/cmd/task@latest
```

### Build Linux amd64 binary

```sh
task build-amd64
```

Output: `build/diskman-web.amd64`

### Build Linux multi-arch binaries

```sh
task build-all
```

Outputs:
- `build/diskman-web.amd64`
- `build/diskman-web.arm64`
- `build/diskman-web.arm`

Build tasks in `Taskfile.yml` already include required options such as `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH`, and `GOARM=7`.

## License

MIT
