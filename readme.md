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
* **Cross-Platform Space Guard:** Verifies remaining disk space before starting multi-gigabyte downloads on both Unix and Windows systems.

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

### 1. Interactive Search & Download Mode

Search the remote catalog interactively for specific archives:

```bash
./kiwix-zim-updater -search "wikipedia" -dir /path/to/zim/folder

```

**Keybindings in Search Mode:**

* `Up` / `Down` or `Ctrl+P` / `Ctrl+N`: Navigate items
* `Space`: Select/deselect an item
* `Ctrl+A`: Select all matches
* `Esc`: Clear selection
* `Enter`: Confirm selection and start downloading
* `Ctrl+C`: Exit search

---

### 2. Auto-Update Local Archives

Scan a directory containing `.zim` files, check for newer remote versions, and prompt to download updates:

```bash
./kiwix-zim-updater -dir /path/to/zim/folder

```

Skip confirmation prompts and run headlessly:

```bash
./kiwix-zim-updater -dir /path/to/zim/folder -a

```

---

### 3. Advanced Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-dir` | `string` | `.` | Target directory for local `.zim` files |
| `-search` | `string` | `""` | Initial query to trigger interactive search mode |
| `-a` | `bool` | `false` | Automatically confirm downloads without prompting |
| `-j` | `int` | `1` | Number of parallel download workers |
| `-limit` | `int64` | `0` | Bandwidth limit per worker in MiB/s (`0` = unlimited) |
| `-check-size` | `bool` | `false` | Verify remote file size via HTTP HEAD during update scan |

---

## Examples

**Download with 4 concurrent workers limited to 10 MiB/s per worker:**

```bash
./kiwix-zim-updater -search "stackexchange" -dir ./kiwix-data -j 4 -limit 10

```

**Headless automated update scan with file size validation:**

```bash
./kiwix-zim-updater -dir /var/media/kiwix -a -check-size

```

---

## License

This project is licensed under the MIT License.