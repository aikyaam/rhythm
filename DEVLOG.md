# Rhythm Development Log

## Summary of What We Added Today

### 1. High-Resolution Album Artwork & Thumbnail Engine
- **Multiple Image Protocol Renderers**: Added support for 5 different image rendering modes in the terminal:
  - **Halfblocks**: 24-bit TrueColor block rendering compatible with virtually every terminal.
  - **Braille**: High-density 44x44 resolution dithered rendering.
  - **Sixel**: Native high-res DEC Sixel graphics protocol.
  - **Kitty**: Modern APC graphics protocol for Kitty and compatible terminals.
  - **iTerm2**: Inline graphics protocol for iTerm2, WezTerm, and compatible terminals.
- **Cycle Mode Shortcut (`v`)**: Pressing `v` in the player instantly switches between Halfblocks, Braille, Sixel, Kitty, iTerm2, or hides the artwork.
- **Official Cover Art Lookup**: Integrated automatic search with the iTunes API to pull official, crisp 600x600 square album art for songs.
- **Smart Letterbox Auto-Cropping**: Automatically removes black bars from YouTube 4:3 thumbnails and centers the artwork to a 1:1 square ratio.

### 2. Standalone Cover Artwork Window (`w`)
- Pressing `w` opens a dedicated high-resolution album artwork window alongside the terminal.
- Dynamically updates with the current playing track's artwork and title.
- Pressing `w` again smoothly hides or toggles the window.

### 3. Complete Keyboard Shortcuts & Navigation Fixes
- **Shuffle & Sort (`s` / `S`)**:
  - Pressing `s` or `Ctrl+S` immediately toggles shuffle playback.
  - Multi-key sort commands (`s t`, `s a`, `s d`, `s r`) sort by Title, Artist, Duration, or Reverse order.
  - Capital `S` directly triggers track sorting.
- **Actions Menu (`o`, `Ctrl+Space`)**:
  - Fixed `Ctrl+Space` and `o` across various terminal emulators to open track actions.
- **Playlists & Queue (`Enter`)**:
  - Pressing `Enter` on any playlist now instantly loads and plays that playlist.
  - Pressing `Enter` in the Queue jumps directly to that song in queue.
- **Quit & Back Navigation (`q`, `Esc`)**:
  - Pressing `q` now takes you back to the Home tab or closes popups, and pressing `q` on Home safely quits the app.
- **Quick Theme Switcher (`t`, `T`)**:
  - Pressing either `t` or `T` opens the theme switcher.
- **Universal Favorite Key (`f`)**:
  - Toggles favorite on the highlighted track, or falls back to the currently playing song if no row is selected.

### 4. Built-in Color Themes
- Added 8 curated color themes switchable on the fly:
  - Tokyo Night
  - Catppuccin Mocha
  - Dracula
  - Gruvbox Dark
  - Nord
  - Cyberpunk
  - Rose Pine
  - Sunset Wave

### 5. Live Synced Lyrics View
- Real-time synced lyric scrolling synchronized with song progress.
- Karaoke-style active line indicators.
- Quick timing offset tuning (`[` and `]`) to adjust lyric delays by ±250ms on the fly.
