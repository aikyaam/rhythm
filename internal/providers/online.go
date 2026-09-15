package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

type MusicProvider interface {
	Name() string
	Search(query string, limit int) ([]core.Track, error)
	Resolve(track *core.Track) (string, error)
}

type CompositeProvider struct {
	providers  []MusicProvider
	soundcloud *SoundCloudProvider
	jiosaavn   *JioSaavnProvider
	monochrome *MonochromeProvider
	gaana      *GaanaProvider
	songlink   *SongLinkProvider
	archive    *ArchiveProvider
}

func NewDefaultProvider() *CompositeProvider {
	sc := NewSoundCloudProvider()
	js := NewJioSaavnProvider()
	mc := NewMonochromeProvider()
	gn := NewGaanaProvider(sc)
	sl := NewSongLinkProvider(sc)
	arch := NewArchiveProvider()

	all := []MusicProvider{
		sc,
		js,
		gn,
		mc,
		sl,
		arch,
	}

	return &CompositeProvider{
		providers:  all,
		soundcloud: sc,
		jiosaavn:   js,
		monochrome: mc,
		gaana:      gn,
		songlink:   sl,
		archive:    arch,
	}
}

func (c *CompositeProvider) Name() string {
	return "Composite Online Provider"
}

func (c *CompositeProvider) hasSearchPrefix(q string) bool {
	prefixes := []string{
		"scsearch:", "sc:",
		"jssearch:", "js:",
		"mcsearch:", "mc:", "tidal:",
		"gnsearch:", "gn:", "gaanasearch:",
		"slsearch:", "sl:", "songlink:", "odesli:",
		"archsearch:", "arch:", "archive:",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(q, p) {
			return true
		}
	}
	return false
}

func (c *CompositeProvider) Search(query string, limit int) ([]core.Track, error) {
	trimmed := strings.TrimSpace(query)

	if strings.HasPrefix(trimmed, "scsearch:") || strings.HasPrefix(trimmed, "sc:") || strings.Contains(trimmed, "soundcloud.com") || strings.Contains(trimmed, "on.soundcloud.com") {
		return c.soundcloud.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "jssearch:") || strings.HasPrefix(trimmed, "js:") || strings.Contains(trimmed, "jiosaavn.com") {
		return c.jiosaavn.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "mcsearch:") || strings.HasPrefix(trimmed, "mc:") || strings.HasPrefix(trimmed, "tidal:") || strings.Contains(trimmed, "monochrome.tf") || strings.Contains(trimmed, "tidal.com") {
		return c.monochrome.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "gnsearch:") || strings.HasPrefix(trimmed, "gn:") || strings.HasPrefix(trimmed, "gaanasearch:") || strings.Contains(trimmed, "gaana.com") {
		return c.gaana.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "slsearch:") || strings.HasPrefix(trimmed, "sl:") || strings.HasPrefix(trimmed, "songlink:") || strings.HasPrefix(trimmed, "odesli:") || strings.Contains(trimmed, "song.link") || strings.Contains(trimmed, "odesli.co") {
		return c.songlink.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "archsearch:") || strings.HasPrefix(trimmed, "arch:") || strings.HasPrefix(trimmed, "archive:") || strings.Contains(trimmed, "archive.org") {
		clean := strings.TrimPrefix(trimmed, "archsearch:")
		clean = strings.TrimPrefix(clean, "archive:")
		clean = strings.TrimPrefix(clean, "arch:")
		return c.archive.Search(strings.TrimSpace(clean), limit)
	}

	unified := c.SearchUnified(trimmed, limit)
	if len(unified) > 0 {
		return unified, nil
	}

	return nil, fmt.Errorf("no search results from online providers for query: %s", query)
}

func (c *CompositeProvider) SearchUnified(query string, limit int) []core.Track {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}

	if c.hasSearchPrefix(trimmed) || strings.Contains(trimmed, "://") {
		res, err := c.Search(trimmed, limit)
		if err == nil && len(res) > 0 {
			return res
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var results []core.Track

	perLimit := limit / 2
	if perLimit < 5 {
		perLimit = 5
	}

	searchers := []func(){
		func() {
			defer func() { recover() }()
			scTracks, err := c.soundcloud.Search(trimmed, perLimit)
			if err == nil && len(scTracks) > 0 {
				mu.Lock()
				results = append(results, scTracks...)
				mu.Unlock()
			}
		},
		func() {
			defer func() { recover() }()
			jsTracks, err := c.jiosaavn.Search(trimmed, perLimit)
			if err == nil && len(jsTracks) > 0 {
				mu.Lock()
				results = append(results, jsTracks...)
				mu.Unlock()
			}
		},
		func() {
			defer func() { recover() }()
			gnTracks, err := c.gaana.Search(trimmed, perLimit)
			if err == nil && len(gnTracks) > 0 {
				mu.Lock()
				results = append(results, gnTracks...)
				mu.Unlock()
			}
		},
		func() {
			defer func() { recover() }()
			mcTracks, err := c.monochrome.Search(trimmed, perLimit)
			if err == nil && len(mcTracks) > 0 {
				mu.Lock()
				results = append(results, mcTracks...)
				mu.Unlock()
			}
		},
	}

	wg.Add(len(searchers))
	for _, fn := range searchers {
		go func(f func()) {
			defer wg.Done()
			f()
		}(fn)
	}

	wg.Wait()
	return results
}

func (c *CompositeProvider) Resolve(track *core.Track) (string, error) {
	for _, p := range c.providers {
		if strings.EqualFold(p.Name(), track.RemoteReference) {
			streamURL, err := p.Resolve(track)
			if err == nil && streamURL != "" && !strings.Contains(streamURL, "AudioPreview") && !strings.Contains(streamURL, "mzaf_") {
				return streamURL, nil
			}
		}
	}

	streamProviders := []MusicProvider{
		c.jiosaavn,
		c.soundcloud,
		c.monochrome,
		c.gaana,
	}

	for _, p := range streamProviders {
		streamURL, err := p.Resolve(track)
		if err == nil && streamURL != "" && !strings.Contains(streamURL, "AudioPreview") {
			return streamURL, nil
		}
	}

	query := fmt.Sprintf("%s %s", track.Title, track.Artist)
	for _, p := range streamProviders {
		candidates, err := p.Search(query, 3)
		if err == nil && len(candidates) > 0 {
			streamURL, err := p.Resolve(&candidates[0])
			if err == nil && streamURL != "" && !strings.Contains(streamURL, "AudioPreview") {
				return streamURL, nil
			}
		}
	}

	return "", fmt.Errorf("failed to resolve full audio stream for track: %s", track.Title)
}

type ArchiveProvider struct {
	client *http.Client
}

func NewArchiveProvider() *ArchiveProvider {
	return &ArchiveProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *ArchiveProvider) Name() string {
	return "Archive.org"
}

type archiveResponse struct {
	Response struct {
		Docs []struct {
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Creator    string `json:"creator"`
			Year       string `json:"year"`
			Mediatype  string `json:"mediatype"`
		} `json:"docs"`
	} `json:"response"`
}

func (a *ArchiveProvider) Search(query string, limit int) ([]core.Track, error) {
	if limit <= 0 {
		limit = 10
	}

	apiURL := fmt.Sprintf(
		"https://archive.org/advancedsearch.php?q=%s+AND+mediatype:audio&fl[]=identifier,title,creator,year,mediatype&rows=%d&output=json",
		url.QueryEscape(query), limit,
	)

	resp, err := a.client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data archiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var tracks []core.Track
	for _, doc := range data.Response.Docs {
		artist := doc.Creator
		if artist == "" {
			artist = "Archive Artist"
		}
		title := doc.Title
		if title == "" {
			title = doc.Identifier
		}

		t := core.Track{
			ID:              uuid.New().String(),
			Title:           title,
			Artist:          artist,
			Album:           "Internet Archive",
			Source:          core.SourceOnline,
			SourceID:        doc.Identifier,
			RemoteReference: a.Name(),
			DateAdded:       time.Now(),
		}
		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (a *ArchiveProvider) Resolve(track *core.Track) (string, error) {
	streamURL := fmt.Sprintf("https://archive.org/download/%s", track.SourceID)
	return streamURL, nil
}
