package lyrics

import (
	"testing"
	"time"

	"rhythm/internal/core"
)

func TestParseTrackQuery(t *testing.T) {
	cases := []struct {
		input  string
		artist string
		title  string
	}{
		{"The Weeknd - Starboy (Official Music Video)", "The Weeknd", "Starboy"},
		{"Taylor Swift - Anti-Hero (Lyrics)", "Taylor Swift", "Anti-Hero"},
		{"Joji – SLOW DANCING IN THE DARK [Visualizer]", "Joji", "SLOW DANCING IN THE DARK"},
		{"Plain Title Without Separator", "", "Plain Title Without Separator"},
	}

	for _, c := range cases {
		art, tit := parseTrackQuery(c.input)
		if art != c.artist {
			t.Errorf("input %q: expected artist %q, got %q", c.input, c.artist, art)
		}
		if tit != c.title {
			t.Errorf("input %q: expected title %q, got %q", c.input, c.title, tit)
		}
	}
}

func TestResolveArtistAndTitle(t *testing.T) {
	track1 := &core.Track{
		Title:  "The Weeknd - Starboy (Official Video)",
		Artist: "The Weeknd",
	}
	art, tit := resolveArtistAndTitle(track1)
	if art != "The Weeknd" || tit != "Starboy" {
		t.Errorf("expected The Weeknd / Starboy, got %s / %s", art, tit)
	}

	track2 := &core.Track{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
	}
	art, tit = resolveArtistAndTitle(track2)
	if art != "The Weeknd" || tit != "Blinding Lights" {
		t.Errorf("expected The Weeknd / Blinding Lights, got %s / %s", art, tit)
	}

	track3 := &core.Track{
		Title:  "Dua Lipa - Levitating (feat. DaBaby)",
		Artist: "YouTube",
	}
	art, tit = resolveArtistAndTitle(track3)
	if art != "Dua Lipa" || tit != "Levitating" {
		t.Errorf("expected Dua Lipa / Levitating, got %s / %s", art, tit)
	}
}

func TestSelectBestLRCLIBMatch(t *testing.T) {
	results := []lrclibSearchItem{
		{
			TrackName:    "Unrelated Song",
			ArtistName:   "The Weeknd",
			Duration:     230,
			SyncedLyrics: "[00:10.00] Random text",
		},
		{
			TrackName:    "Starboy",
			ArtistName:   "Someone Else Cover",
			Duration:     150,
			SyncedLyrics: "[00:10.00] Cover text",
		},
		{
			TrackName:    "Starboy",
			ArtistName:   "The Weeknd",
			Duration:     230,
			SyncedLyrics: "[00:10.00] Starboy official text",
		},
	}

	best := selectBestLRCLIBMatch(results, "Starboy", "The Weeknd", 230)
	if best == nil {
		t.Fatalf("expected a match, got nil")
	}
	if best.ArtistName != "The Weeknd" || best.TrackName != "Starboy" {
		t.Fatalf("expected Starboy by The Weeknd, got %s - %s", best.ArtistName, best.TrackName)
	}

	mismatched := selectBestLRCLIBMatch(results, "Completely Different Song", "The Weeknd", 230)
	if mismatched != nil {
		t.Fatalf("expected nil for mismatched song, got %v", mismatched)
	}
}

func TestParseLetrasSubtitle(t *testing.T) {
	subRaw := "[[\"First line\", \"1.5\", \"3.0\"], [\"Second line\", \"4.0\", \"6.2\"]]"
	lines := parseLetrasSubtitle(subRaw)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Text != "First line" || lines[0].Time != 1500*time.Millisecond {
		t.Errorf("unexpected first line: %+v", lines[0])
	}
	if lines[1].Text != "Second line" || lines[1].Time != 4000*time.Millisecond {
		t.Errorf("unexpected second line: %+v", lines[1])
	}
}
