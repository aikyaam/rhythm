package metadata

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"runtime"
	"strings"
	"sync"

	"rhythm/internal/core"
)

type ImageProtocol string

const (
	ProtocolAuto       ImageProtocol = "auto"
	ProtocolSixel      ImageProtocol = "sixel"
	ProtocolBraille    ImageProtocol = "braille"
	ProtocolKitty      ImageProtocol = "kitty"
	ProtocolITerm2     ImageProtocol = "iterm2"
	ProtocolHalfblocks ImageProtocol = "halfblocks"
)

var (
	protoCache   = make(map[string][]string)
	protoCacheMu sync.RWMutex
)

func DetectImageProtocol() ImageProtocol {
	termProg := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))
	lcTerm := strings.ToLower(os.Getenv("LC_TERMINAL"))

	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("KITTY_PID") != "" || term == "xterm-kitty" || termProg == "kitty" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || term == "xterm-ghostty" || termProg == "ghostty" {
		return ProtocolKitty
	}

	if termProg == "iterm.app" || lcTerm == "iterm2" || termProg == "iterm2" || termProg == "wezterm" || os.Getenv("WEZTERM_EXECUTABLE") != "" || os.Getenv("WEZTERM_PANE") != "" {
		return ProtocolITerm2
	}

	if runtime.GOOS == "windows" || os.Getenv("WT_SESSION") != "" || termProg == "mintty" || term == "foot" || term == "foot-extra" || termProg == "mlterm" || os.Getenv("XTERM_VERSION") != "" || strings.Contains(term, "sixel") {
		return ProtocolSixel
	}

	if runtime.GOOS == "darwin" {
		return ProtocolITerm2
	}

	return ProtocolSixel
}

func ResolveProtocol(p ImageProtocol) ImageProtocol {
	if p == ProtocolAuto || p == "" {
		return DetectImageProtocol()
	}
	switch strings.ToLower(string(p)) {
	case "sixel":
		return ProtocolSixel
	case "braille":
		return ProtocolBraille
	case "kitty":
		return ProtocolKitty
	case "iterm2", "iterm":
		return ProtocolITerm2
	case "halfblock", "halfblocks", "blocks":
		return ProtocolHalfblocks
	default:
		return DetectImageProtocol()
	}
}

func ProtocolDisplayName(p ImageProtocol) string {
	switch ResolveProtocol(p) {
	case ProtocolSixel:
		return "Sixel (DEC High-Res Graphics - needs Sixel terminal)"
	case ProtocolBraille:
		return "Braille (High-Density TrueColor 44x44)"
	case ProtocolKitty:
		return "Kitty (Graphics Protocol)"
	case ProtocolITerm2:
		return "iTerm2 (Inline Images)"
	default:
		return "Halfblocks (24-bit TrueColor)"
	}
}

func NextImageProtocol(current ImageProtocol) ImageProtocol {
	switch current {
	case ProtocolSixel:
		return ProtocolBraille
	case ProtocolBraille:
		return ProtocolHalfblocks
	case ProtocolHalfblocks:
		return ProtocolKitty
	case ProtocolKitty:
		return ProtocolITerm2
	case ProtocolITerm2:
		return ProtocolSixel
	default:
		return ProtocolSixel
	}
}

func EncodeSixel(img image.Image, targetCols, targetRows int) []string {
	if img == nil || targetCols <= 0 || targetRows <= 0 {
		return nil
	}

	pixWidth := targetCols * 8
	pixHeight := targetRows * 16

	bounds := img.Bounds()
	bW := bounds.Dx()
	bH := bounds.Dy()
	if bW <= 0 || bH <= 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\x1bP7;1;100q\"1;1;%d;%d", pixWidth, pixHeight))

	grid := make([][]int, pixHeight)
	usedColors := make(map[int]color.RGBA)

	for y := 0; y < pixHeight; y++ {
		grid[y] = make([]int, pixWidth)
		for x := 0; x < pixWidth; x++ {
			sx := bounds.Min.X + (x*bW)/pixWidth
			sy := bounds.Min.Y + (y*bH)/pixHeight
			c := img.At(sx, sy)
			r, g, b, _ := c.RGBA()
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)

			rIdx := int(r8) * 5 / 255
			gIdx := int(g8) * 5 / 255
			bIdx := int(b8) * 5 / 255
			cIdx := rIdx*36 + gIdx*6 + bIdx

			grid[y][x] = cIdx
			if _, ok := usedColors[cIdx]; !ok {
				usedColors[cIdx] = color.RGBA{
					R: uint8((int(r8) * 100) / 255),
					G: uint8((int(g8) * 100) / 255),
					B: uint8((int(b8) * 100) / 255),
					A: 255,
				}
			}
		}
	}

	for cIdx, rgba := range usedColors {
		sb.WriteString(fmt.Sprintf("#%d;2;%d;%d;%d", cIdx, rgba.R, rgba.G, rgba.B))
	}

	for y := 0; y < pixHeight; y += 6 {
		sliceColors := make(map[int]bool)
		for dy := 0; dy < 6 && y+dy < pixHeight; dy++ {
			for x := 0; x < pixWidth; x++ {
				sliceColors[grid[y+dy][x]] = true
			}
		}

		colorCount := 0
		totalInSlice := len(sliceColors)
		for cIdx := range sliceColors {
			colorCount++
			sb.WriteString(fmt.Sprintf("#%d", cIdx))

			var prevChar byte = 0
			runLen := 0

			flushRun := func() {
				if runLen == 0 {
					return
				}
				if runLen == 1 {
					sb.WriteByte(prevChar)
				} else if runLen == 2 {
					sb.WriteByte(prevChar)
					sb.WriteByte(prevChar)
				} else {
					sb.WriteString(fmt.Sprintf("!%d%c", runLen, prevChar))
				}
				runLen = 0
			}

			for x := 0; x < pixWidth; x++ {
				mask := 0
				for dy := 0; dy < 6; dy++ {
					py := y + dy
					if py < pixHeight && grid[py][x] == cIdx {
						mask |= (1 << dy)
					}
				}
				ch := byte(63 + mask)
				if ch == prevChar {
					runLen++
				} else {
					flushRun()
					prevChar = ch
					runLen = 1
				}
			}
			flushRun()

			if colorCount < totalInSlice {
				sb.WriteByte('$')
			}
		}

		if y+6 < pixHeight {
			sb.WriteByte('-')
		}
	}

	sb.WriteString("\x1b\\")

	sixelSeq := sb.String()

	lines := make([]string, targetRows)
	emptyPad := strings.Repeat(" ", targetCols)
	for i := 0; i < targetRows; i++ {
		lines[i] = emptyPad
	}
	lines[0] = "\x1b7" + sixelSeq + "\x1b8" + emptyPad

	return lines
}

func EncodeKitty(img image.Image, targetCols, targetRows int) []string {
	if img == nil || targetCols <= 0 || targetRows <= 0 {
		return nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}

	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	kittySeq := fmt.Sprintf("\x1b_Ga=T,f=100,t=d,c=%d,r=%d,m=0;%s\x1b\\", targetCols, targetRows, b64)

	lines := make([]string, targetRows)
	emptyPad := strings.Repeat(" ", targetCols)
	for i := 0; i < targetRows; i++ {
		lines[i] = emptyPad
	}
	lines[0] = kittySeq + emptyPad
	return lines
}

func EncodeITerm2(img image.Image, targetCols, targetRows int) []string {
	if img == nil || targetCols <= 0 || targetRows <= 0 {
		return nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}

	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	itermSeq := fmt.Sprintf("\x1b]1337;File=inline=1;preserveAspectRatio=1;width=%d;height=%d:%s\x07", targetCols, targetRows, b64)

	lines := make([]string, targetRows)
	emptyPad := strings.Repeat(" ", targetCols)
	for i := 0; i < targetRows; i++ {
		lines[i] = emptyPad
	}
	lines[0] = itermSeq + emptyPad
	return lines
}

func GetTrackCoverThumbnailProto(track *core.Track, targetCols, targetRows int, proto ImageProtocol) []string {
	resolved := ResolveProtocol(proto)
	if track == nil {
		return GenerateFallbackArtwork(targetCols, targetRows, "Rhythm", "")
	}

	cacheKey := track.ID
	if cacheKey == "" {
		cacheKey = track.SourceID
	}
	if cacheKey == "" {
		cacheKey = track.LocalPath
	}
	fullKey := fmt.Sprintf("thumb_p%s_%s_%dx%d", resolved, cacheKey, targetCols, targetRows)

	protoCacheMu.RLock()
	if cached, ok := protoCache[fullKey]; ok {
		protoCacheMu.RUnlock()
		return cached
	}
	protoCacheMu.RUnlock()

	img, err := loadTrackImage(track)
	if err == nil && img != nil {
		var lines []string
		switch resolved {
		case ProtocolSixel:
			lines = EncodeSixel(img, targetCols, targetRows)
		case ProtocolKitty:
			lines = EncodeKitty(img, targetCols, targetRows)
		case ProtocolITerm2:
			lines = EncodeITerm2(img, targetCols, targetRows)
		case ProtocolBraille:
			lines = ImageToBraille(img, targetCols, targetRows)
		case ProtocolHalfblocks:
			fallthrough
		default:
			lines = ImageToHalfBlock(img, targetCols, targetRows)
		}

		if len(lines) == targetRows {
			protoCacheMu.Lock()
			protoCache[fullKey] = lines
			protoCacheMu.Unlock()
			return lines
		}
	}

	fallback := GenerateFallbackArtwork(targetCols, targetRows, track.Title, track.Artist)
	protoCacheMu.Lock()
	protoCache[fullKey] = fallback
	protoCacheMu.Unlock()
	return fallback
}

func GetCachedThumbnailProto(track *core.Track, targetCols, targetRows int, proto ImageProtocol) []string {
	resolved := ResolveProtocol(proto)
	if track == nil {
		return nil
	}
	cacheKey := track.ID
	if cacheKey == "" {
		cacheKey = track.SourceID
	}
	if cacheKey == "" {
		cacheKey = track.LocalPath
	}
	fullKey := fmt.Sprintf("thumb_p%s_%s_%dx%d", resolved, cacheKey, targetCols, targetRows)

	protoCacheMu.RLock()
	defer protoCacheMu.RUnlock()
	return protoCache[fullKey]
}
