# Kiwix ZIM Updater

A fast, concurrent CLI tool written in Go to search, download, and update offline Kiwix ZIM archives with interactive TUI search, rate limiting, and automated checksum verification.

---

## Features

* **Interactive TUI Search:** Fuzzy search and select ZIM archives directly from the online Kiwix catalog using an interactive terminal interface.
* **Automated Directory Updates:** Scans local directories for outdated `.zim` files and automatically fetches newer releases.
* **Concurrent Worker Engine:** Download multiple files in parallel with configurable worker limits.
* **Bandwidth Control:** Built-in rate limiter to throttle download speed per worker.
* **SHA256 Integrity Checks:** Automatically verifies downloaded archives against remote checksum sidecars.
* **Resumable Downloads:** Supports HTTP Range requests to resume interrupted downloads cleanly.
* **Cross-Platform Space Guard:** Verifies remaining disk space before starting multi-gigabyte downloads on Unix and Windows systems.

---

## Prerequisites

* **Go:** 1.20 or later

---

## Installation

Clone the repository and build the binary:

```bash
# Clone the repository
git clone https://github.com/evrial/kiwix-zim-updater.git
cd kiwix-zim-updater

# Build the executable for your current OS
go build -o kiwix-zim-updater .

```

---

## Usage

### 1. Interactive Search Mode (`search`)

Launch the interactive search interface to browse and multi-select archives from the Kiwix catalog:

```bash
# Open blank interactive search TUI
./kiwix-zim-updater search -dir /path/to/zim/folder

# Open search TUI pre-filtered with a query term
./kiwix-zim-updater search "wikipedia" -dir /path/to/zim/folder

```

**Keybindings in Search Mode:**

* `Up` / `Down` or `Ctrl+P` / `Ctrl+N`: Navigate items
* `Space`: Select/deselect item
* `Ctrl+A`: Select all visible matches
* `Esc`: Clear selected items
* `Enter`: Confirm selection and start downloading
* `Ctrl+C`: Exit search

---

### 2. Auto-Update Local Archives (`update` / default)

Scan a directory containing local `.zim` files, compare against remote versions, and download updates:

```bash
# Run update check with confirmation prompt
./kiwix-zim-updater update -dir /path/to/zim/folder

# Running without subcommands defaults to update mode
./kiwix-zim-updater -dir /path/to/zim/folder

```

Skip confirmation prompts and run headlessly:

```bash
./kiwix-zim-updater update -dir /path/to/zim/folder -a

```

---

## Flags Reference

### `search` Subcommand Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-dir` | `string` | `.` | Directory to save downloaded `.zim` files |
| `-j` | `int` | `1` | Number of parallel download workers |
| `-limit` | `int64` | `0` | Bandwidth limit per worker in MiB/s (`0` = unlimited) |
| `-a` | `bool` | `false` | Skip download confirmation prompt after selection |

### `update` Subcommand Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-dir` | `string` | `.` | Target directory containing local `.zim` files |
| `-a` | `bool` | `false` | Automatically proceed with downloads without prompting |
| `-j` | `int` | `1` | Number of parallel download workers |
| `-limit` | `int64` | `0` | Bandwidth limit per worker in MiB/s (`0` = unlimited) |
| `-check-size` | `bool` | `false` | Perform HTTP HEAD requests to verify remote file size integrity |

---

## Examples

**Search for StackExchange archives with 4 workers limited to 10 MiB/s each:**

```bash
./kiwix-zim-updater search "stackexchange" -dir ./kiwix-data -j 4 -limit 10

```

**Headless automated update scan with file size validation:**

```bash
./kiwix-zim-updater update -dir /var/media/kiwix -a -check-size

```

---

## License

This project is licensed under the MIT License.