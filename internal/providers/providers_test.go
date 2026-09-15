package providers

import (
	"strings"
	"testing"
)

func TestAllProvidersInitialization(t *testing.T) {
	sc := NewSoundCloudProvider()
	if sc.Name() != "SoundCloud" {
		t.Errorf("expected SoundCloud, got %s", sc.Name())
	}

	js := NewJioSaavnProvider()
	if js.Name() != "JioSaavn" {
		t.Errorf("expected JioSaavn, got %s", js.Name())
	}

	mc := NewMonochromeProvider()
	if mc.Name() != "Monochrome" {
		t.Errorf("expected Monochrome, got %s", mc.Name())
	}

	gn := NewGaanaProvider(sc)
	if gn.Name() != "Gaana" {
		t.Errorf("expected Gaana, got %s", gn.Name())
	}

	sl := NewSongLinkProvider(sc)
	if sl.Name() != "SongLink" {
		t.Errorf("expected SongLink, got %s", sl.Name())
	}

	arch := NewArchiveProvider()
	if arch.Name() != "Archive.org" {
		t.Errorf("expected Archive.org, got %s", arch.Name())
	}
}

func TestCompositeProvider(t *testing.T) {
	comp := NewDefaultProvider()
	if len(comp.providers) != 6 {
		t.Fatalf("expected 6 providers registered, got %d", len(comp.providers))
	}

	expectedNames := []string{
		"SoundCloud",
		"JioSaavn",
		"Monochrome",
		"Gaana",
		"SongLink",
		"Archive.org",
	}

	foundMap := make(map[string]bool)
	for _, p := range comp.providers {
		foundMap[p.Name()] = true
	}

	for _, name := range expectedNames {
		if !foundMap[name] {
			t.Errorf("missing provider: %s", name)
		}
	}
}

func TestURLRegexPatterns(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://soundcloud.com/artist/track-name", true},
		{"https://on.soundcloud.com/abc12", true},
		{"https://gaana.com/song/dil-bechara", true},
		{"https://gaana.com/album/rockstar", true},
		{"https://tidal.com/browse/track/12345678", true},
		{"https://song.link/i/1440857781", true},
		{"https://odesli.co/s/12345", true},
	}

	for _, tt := range tests {
		matched := scTrackURLRegex.MatchString(tt.url) ||
			scShortURLRegex.MatchString(tt.url) ||
			gaanaURLRegex.MatchString(tt.url) ||
			monochromeURLRegex.MatchString(tt.url) ||
			songLinkURLRegex.MatchString(tt.url)

		if matched != tt.expected {
			t.Errorf("URL match failed for %s: got %v, expected %v", tt.url, matched, tt.expected)
		}
	}
}

func TestSoundCloudClientIDExtraction(t *testing.T) {
	mockScript := `client_id=b1gYhJbF15a1Uj5eLzR1Kj6aM1e9nC3d&app_version=1700000`
	match := scClientIDRegex.FindStringSubmatch(mockScript)
	if len(match) < 2 || match[1] != "b1gYhJbF15a1Uj5eLzR1Kj6aM1e9nC3d" {
		t.Errorf("failed to extract client ID from mock script")
	}
}

func TestGaanaDecryptStream(t *testing.T) {
	g := NewGaanaProvider(nil)
	empty := g.decryptStreamPath("")
	if empty != "" {
		t.Errorf("expected empty string for empty input")
	}
	invalid := g.decryptStreamPath("short")
	if invalid != "" {
		t.Errorf("expected empty string for invalid input")
	}
}

func TestPrefixTrimming(t *testing.T) {
	prefixes := []string{
		"scsearch:test query",
		"jssearch:test query",
		"mcsearch:test query",
		"gnsearch:test query",
		"slsearch:test query",
		"archsearch:test query",
	}

	for _, p := range prefixes {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 || parts[1] != "test query" {
			t.Errorf("failed to split prefix: %s", p)
		}
	}
}

func TestLiveSoundCloudAndJioSaavnSearch(t *testing.T) {
	comp := NewDefaultProvider()
	tracks, err := comp.Search("tum mere", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatalf("expected tracks, got 0")
	}
	t.Logf("Found %d tracks. First track: %s - %s from %s", len(tracks), tracks[0].Title, tracks[0].Artist, tracks[0].RemoteReference)
}
