# Rhythm 🎵

**Rhythm** is a high-performance terminal music player and personal music cloud designed for audiophiles, developers, and command-line enthusiasts.

## ✨ Features

- **Terminal TUI & CLI**: Interactive Bubble Tea interface with responsive layouts, real-time audio spectrum visualization, and vim-inspired navigation.
- **Synchronized Live Lyrics**: LRCLIB integration providing real-time synchronized karaoke-style lyric scrolling.
- **Multi-Source Audio Engine**: Seamlessly plays local files (FLAC, MP3, WAV, AAC, Opus, Vorbis) and streams from online providers (YouTube, Spotify metadata delegation, JioSaavn 320kbps).
- **Personal Music Cloud / NAS**: Built-in HTTP streaming server with one-button remote acquisition to save online streams directly to your home server.
- **Hardware Acceleration**: Windows native WinRT MediaPlayer playback with pure Go software fallback.
- **ASCII Art Generator**: Convert album art and custom images into ANSI/ASCII art.

## 🚀 Installation & Building

```bash
# Build Rhythm Client
go build -o bin/rhythm ./cmd/rhythm

# Build Rhythm Remote NAS Server
go build -o bin/rhythm-server ./cmd/rhythm-server
```

## ⌨️ Quickstart & Usage

```bash
# Launch interactive TUI player
rhythm

# Run system diagnostic checks
rhythm doctor

# Search local library and online providers
rhythm search "Miles Davis"

# Direct audio playback
rhythm play "Song Name or URL"

# Manage playlists & favorites
rhythm favorites
rhythm playlist list
```

## 📄 License

MIT
