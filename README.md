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

- Go 1.22+
- Linux (uses `ddrescue`, `dd`, SIGUSR1)
- [`ddrescue`](https://www.gnu.org/software/ddrescue/) for copy operations
- `dd` (GNU coreutils) for erase operations

## Installation

```sh
git clone https://github.com/yourname/diskman-web
cd diskman-web
go build -o diskman-web .
```

## Usage

```
./diskman-web [flags]

Flags:
  -config    <path>   Path to config file (default: platform config dir)
  -enclosure <name>   Active enclosure name (overrides config activeEnclosure)
  -addr      <addr>   Listen address, e.g. :8080 (overrides config)
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
        "1": "/dev/sda",
        "2": "/dev/sdb",
        "3": "/dev/sdc",
        "4": "/dev/sdd",
        "5": "/dev/sde",
        "6": "/dev/sdf"
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

## License

MIT
