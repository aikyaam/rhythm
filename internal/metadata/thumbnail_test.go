package metadata

import (
	"image"
	"image/color"
	"os"
	"strings"
	"testing"

	"rhythm/internal/core"
)

func createTestImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / w),
				G: uint8((y * 255) / h),
				B: 128,
				A: 255,
			})
		}
	}
	return img
}

func TestThumbnailGeneration(t *testing.T) {
	track := &core.Track{
		ID:     "test-123",
		Title:  "Starboy",
		Artist: "The Weeknd",
	}

	lines := GetTrackCoverThumbnail(track, 22, 11)
	if len(lines) != 11 {
		t.Fatalf("expected 11 lines, got %d", len(lines))
	}

	cached := GetCachedThumbnail(track, 22, 11)
	if len(cached) != 11 {
		t.Fatalf("expected cached lines to have 11 lines, got %d", len(cached))
	}

	fallback := GenerateFallbackArtwork(22, 11, "Unknown", "Unknown")
	if len(fallback) != 11 {
		t.Fatalf("expected fallback to have 11 lines, got %d", len(fallback))
	}
}

func TestImageProtocols(t *testing.T) {
	img := createTestImage(64, 64)

	sixelLines := EncodeSixel(img, 22, 11)
	if len(sixelLines) != 11 {
		t.Fatalf("expected 11 sixel lines, got %d", len(sixelLines))
	}
	if !strings.Contains(sixelLines[0], "\x1bP7;1;100q") {
		t.Fatalf("expected Sixel DCS header, got: %q", sixelLines[0][:min(25, len(sixelLines[0]))])
	}
	if !strings.Contains(sixelLines[0], "\x1b\\") {
		t.Fatalf("expected Sixel ST terminator in line 0")
	}

	kittyLines := EncodeKitty(img, 22, 11)
	if len(kittyLines) != 11 {
		t.Fatalf("expected 11 kitty lines, got %d", len(kittyLines))
	}
	if !strings.HasPrefix(kittyLines[0], "\x1b_Ga=T") {
		t.Fatalf("expected Kitty APC header, got: %q", kittyLines[0][:min(15, len(kittyLines[0]))])
	}

	itermLines := EncodeITerm2(img, 22, 11)
	if len(itermLines) != 11 {
		t.Fatalf("expected 11 iterm lines, got %d", len(itermLines))
	}
	if !strings.HasPrefix(itermLines[0], "\x1b]1337;File=inline=1") {
		t.Fatalf("expected iTerm2 OSC header, got: %q", itermLines[0][:min(25, len(itermLines[0]))])
	}

	halfblockLines := ImageToHalfBlock(img, 22, 11)
	if len(halfblockLines) != 11 {
		t.Fatalf("expected 11 halfblock lines, got %d", len(halfblockLines))
	}

	p := ProtocolSixel
	p = NextImageProtocol(p)
	if p != ProtocolBraille {
		t.Fatalf("expected ProtocolBraille, got %v", p)
	}
	p = NextImageProtocol(p)
	if p != ProtocolHalfblocks {
		t.Fatalf("expected ProtocolHalfblocks, got %v", p)
	}
	p = NextImageProtocol(p)
	if p != ProtocolKitty {
		t.Fatalf("expected ProtocolKitty, got %v", p)
	}
	p = NextImageProtocol(p)
	if p != ProtocolITerm2 {
		t.Fatalf("expected ProtocolITerm2, got %v", p)
	}
	p = NextImageProtocol(p)
	if p != ProtocolSixel {
		t.Fatalf("expected ProtocolSixel, got %v", p)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestDetectImageProtocol(t *testing.T) {
	origKitty := os.Getenv("KITTY_WINDOW_ID")
	origTermProg := os.Getenv("TERM_PROGRAM")
	defer func() {
		os.Setenv("KITTY_WINDOW_ID", origKitty)
		os.Setenv("TERM_PROGRAM", origTermProg)
	}()

	os.Setenv("KITTY_WINDOW_ID", "123")
	if proto := DetectImageProtocol(); proto != ProtocolKitty {
		t.Fatalf("expected ProtocolKitty, got %v", proto)
	}

	os.Setenv("KITTY_WINDOW_ID", "")
	os.Setenv("TERM_PROGRAM", "iTerm.app")
	if proto := DetectImageProtocol(); proto != ProtocolITerm2 {
		t.Fatalf("expected ProtocolITerm2, got %v", proto)
	}

	if res := ResolveProtocol(ProtocolAuto); res == "" {
		t.Fatalf("expected resolved protocol, got empty")
	}
}
