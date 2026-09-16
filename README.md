[![Release](https://img.shields.io/github/v/release/aikyaam/rhythm?color=6366f1&label=release)](https://github.com/aikyaam/rhythm/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/aikyaam/rhythm.svg)](https://pkg.go.dev/github.com/aikyaam/rhythm)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/aikyaam/rhythm/blob/main/LICENSE)
[![Platform Support](https://img.shields.io/badge/platform-linux%20%7C%20windows%20%7C%20macos-informational)](https://github.com/aikyaam/rhythm/releases)

<img align="right" src="assets/logo.png" width=192 alt="rhythm logo">

# Rhythm

Rhythm is a blazing-fast, terminal-native music player and distributed streaming server written in Go. Engineered with a cyberpunk Bubbletea TUI, a pure native audio decoding pipeline, multi-source streaming resolution, synchronized LRC lyrics, and a headless client-server architecture for home labs and remote workstations.

## Summary

1. [Features](#features)
2. [Getting Started](#getting-started)
3. [Architecture](#architecture)
4. [Documentation](#documentation)
5. [Demo](#demo)
6. [Examples](#examples)
7. [License](#license)

### Features

- [x] **Terminal-Native Bubbletea UI**: Ultra-responsive, modern terminal interface built on the Elm architecture with [Bubbletea](https://github.com/charmbracelet/bubbletea) and styled with [Lipgloss](https://github.com/charmbracelet/lipgloss).
- [x] **Pure Go Audio Engine**: Direct hardware audio output via Oto and Beep with zero external C-dependencies (`CGO_ENABLED=0`) across Linux, macOS, and Windows.
- [x] **Multi-Source Music Resolution**: Unified, seamless playback across streaming providers and local collections:
  - **SoundCloud**: Direct high-bitrate audio streaming with rich artist metadata.
  - **JioSaavn**: High-fidelity 320kbps & 160kbps streaming audio streams.
  - **Gaana**: HLS stream resolution and playback.
  - **Monochrome / Tidal**: High-resolution FLAC and AAC lossless streaming.
  - **SongLink / Odesli**: Universal metadata resolution and cross-platform song tracking.
  - **Archive.org**: Streaming access to millions of public domain recordings and live concerts.
  - **Local Library**: High-performance local library indexer with tag extraction (FLAC, MP3, M4A, WAV, Opus, AAC).
- [x] **Smart Fallback & Zero-Preview Policy**: Transparently bypasses 30-second previews and resolved paywalls by dynamically switching to active lossless and direct stream backends.
- [x] **Synchronized Lyrics**: Automated real-time lyrics fetching with millisecond-accurate highlighting powered by the LRCLIB engine.
- [x] **ASCII Album Art Renderer**: Real-time pixel-to-ANSI half-block artwork conversion dynamically rendered in the terminal window.
- [x] **Distributed Client-Server Mode**: Deploy `rhythm-server` as a headless background daemon on your NAS, Home Lab, or VPS, and control audio output remotely from any terminal.

---

## Getting Started

### Installing Pre-Built Binaries

Pre-compiled statically linked binaries for Linux, macOS, and Windows (amd64 & arm64) are available on our [GitHub Releases](https://github.com/aikyaam/rhythm/releases) page.

Download and unpack the latest release for your platform:

#### Linux & macOS

```bash
# Download latest release (replace with target architecture)
curl -fsSL -o rhythm.tar.gz https://github.com/aikyaam/rhythm/releases/latest/download/rhythm-v1.0.0-linux-amd64.tar.gz
tar -xzf rhythm.tar.gz
sudo mv rhythm-linux-amd64/rhythm /usr/local/bin/
sudo mv rhythm-linux-amd64/rhythm-server /usr/local/bin/
```

#### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/aikyaam/rhythm/releases/latest/download/rhythm-v1.0.0-windows-amd64.zip" -OutFile "rhythm.zip"
Expand-Archive -Path "rhythm.zip" -DestinationPath "$HOME\rhythm"
# Add $HOME\rhythm to your user PATH
```

### Install with Go

If you have Go installed on your workstation:

```bash
# Install TUI Client
go install github.com/aikyaam/rhythm/cmd/rhythm@latest

# Install Headless Server
go install github.com/aikyaam/rhythm/cmd/rhythm-server@latest
```

### Building From Source

```bash
git clone https://github.com/aikyaam/rhythm.git
cd rhythm

# Build TUI client
go build -trimpath -ldflags="-s -w" -o bin/rhythm ./cmd/rhythm

# Build Headless server
go build -trimpath -ldflags="-s -w" -o bin/rhythm-server ./cmd/rhythm-server
```

---

## Architecture

Rhythm can run in two modes depending on your workflow:

- **Standalone Mode**: The default mode. Running `rhythm` boots both the local audio engine and the interactive Bubbletea interface inside your active terminal.
- **Server / Daemon Mode**: Running `rhythm-server` boots a headless HTTP & WebSocket control daemon on your server or NAS. You can connect to it remotely via `rhythm --server http://nas-ip:8080` to manage queues and stream playback.

---

## Documentation

### Command Line Flags

#### `rhythm` (TUI Client)

```text
Usage of rhythm:
  --server string
        Connect to a remote rhythm-server instance (e.g. http://192.168.1.50:8080)
  --config string
        Custom configuration file path (default: ~/.config/rhythm/config.json)
  --version
        Print version information and exit
```

#### `rhythm-server` (Headless Daemon)

```text
Usage of rhythm-server:
  --port int
        Port to listen on (default: 8080)
  --host string
        Host address to bind to (default: 0.0.0.0)
  --music-dir string
        Path to local/NAS music directory to scan and index
  --db string
        Path to persistent SQLite database (default: ~/.config/rhythm/server.db)
```

### Keybindings

| Key | Action |
|:---|:---|
| <kbd>/</kbd> | Open Search Bar across all active providers |
| <kbd>Enter</kbd> | Play selected track / Activate item |
| <kbd>Space</kbd> | Pause / Resume audio playback |
| <kbd>s</kbd> | Stop playback |
| <kbd>j</kbd> / <kbd>↓</kbd> | Navigate down |
| <kbd>k</kbd> / <kbd>↑</kbd> | Navigate up |
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Switch panes (Library, Search, Queue, Lyrics) |
| <kbd>l</kbd> | Toggle synchronized lyrics overlay |
| <kbd>q</kbd> / <kbd>Ctrl+C</kbd> | Quit application |

---

## Demo

Experience Rhythm in action directly from your terminal:

<p align="center">
  <img src="assets/screenshot.png" alt="Rhythm Terminal Interface" width="100%">
</p>

| **Live Synced Lyrics (LRCLIB)** | **Theme Switcher (`T`)** |
|:---:|:---:|
| <img src="assets/screen3.png" width="100%" alt="Synced Lyrics"> | <img src="assets/screen1.png" width="100%" alt="Themes"> |
| **Track Actions Menu (`o`)** | **Shortcuts Overlay (`?`)** |
| <img src="assets/screen4.png" width="100%" alt="Actions Menu"> | <img src="assets/screen2.png" width="100%" alt="Shortcuts"> |

- [x] [**Latest Release Binaries**](https://github.com/aikyaam/rhythm/releases/latest)
- [x] [**Explore Server Setup & Examples**](#examples)

---

## Examples

### 1. Running Standalone Local Music TUI

Simply launch the binary to enter the interactive player:

```bash
rhythm
```

Press <kbd>/</kbd> to search for any song or artist across SoundCloud, JioSaavn, Gaana, and Tidal.

### 2. Running a Home Lab Audio Server on Linux / Raspberry Pi

Run the headless server attached to your sound system or DAC:

```bash
rhythm-server --port 8080 --music-dir /mnt/nas/music
```

Now connect to your home sound system from any laptop or desktop on your network:

```bash
rhythm --server http://raspberrypi.local:8080
```

### 3. Running as a Systemd Service

Create `/etc/systemd/system/rhythm-server.service`:

```ini
[Unit]
Description=Rhythm Headless Audio Server
After=sound.target network.target

[Service]
Type=simple
User=music
ExecStart=/usr/local/bin/rhythm-server --port 8080 --music-dir /srv/music
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now rhythm-server
```

---

## License

Distributed under the [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/aikyaam/rhythm/blob/main/LICENSE). See [LICENSE](LICENSE) for more information.
