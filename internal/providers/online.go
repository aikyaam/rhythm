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
	providers []MusicProvider
	jiosaavn  *JioSaavnProvider
	youtube   *YouTubeProvider
	spotify   *SpotifyProvider
	archive   *ArchiveProvider
}

func NewDefaultProvider() *CompositeProvider {
	yt := NewYouTubeProvider()
	js := NewJioSaavnProvider()
	sp := NewSpotifyProvider(yt)
	arch := NewArchiveProvider()

	return &CompositeProvider{
		providers: []MusicProvider{
			yt,
			sp,
			js,
			arch,
		},
		youtube:  yt,
		spotify:  sp,
		jiosaavn: js,
		archive:  arch,
	}
}

func (c *CompositeProvider) Name() string {
	return "Composite Online Provider"
}

func (c *CompositeProvider) Search(query string, limit int) ([]core.Track, error) {
	trimmed := strings.TrimSpace(query)

	if strings.HasPrefix(trimmed, "spsearch:") || strings.Contains(trimmed, "open.spotify.com") || strings.HasPrefix(trimmed, "spotify:") {
		return c.spotify.Search(trimmed, limit)
	}
	if strings.HasPrefix(trimmed, "ytsearch:") || strings.Contains(trimmed, "youtube.com") || strings.Contains(trimmed, "youtu.be") {
		return c.youtube.Search(strings.TrimPrefix(trimmed, "ytsearch:"), limit)
	}
	if strings.HasPrefix(trimmed, "jssearch:") || strings.Contains(trimmed, "jiosaavn.com") {
		return c.jiosaavn.Search(strings.TrimPrefix(trimmed, "jssearch:"), limit)
	}

	results, err := c.youtube.Search(trimmed, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}

	results, err = c.jiosaavn.Search(trimmed, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}

	results, err = c.archive.Search(trimmed, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}

	return nil, fmt.Errorf("no search results from online providers for query: %s", query)
}

func (c *CompositeProvider) SearchUnified(query string, limit int) []core.Track {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}

	if strings.Contains(trimmed, "open.spotify.com") || strings.HasPrefix(trimmed, "spotify:") {
		res, err := c.spotify.Search(trimmed, limit)
		if err == nil && len(res) > 0 {
			return res
		}
	}

	if strings.Contains(trimmed, "youtube.com") || strings.Contains(trimmed, "youtu.be") {
		res, err := c.youtube.Search(trimmed, limit)
		if err == nil && len(res) > 0 {
			return res
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var results []core.Track

	perLimit := limit
	if perLimit < 5 {
		perLimit = 5
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		ytTracks, err := c.youtube.Search(trimmed, perLimit)
		if err == nil && len(ytTracks) > 0 {
			mu.Lock()
			results = append(results, ytTracks...)
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		jsTracks, err := c.jiosaavn.Search(trimmed, perLimit)
		if err == nil && len(jsTracks) > 0 {
			mu.Lock()
			results = append(results, jsTracks...)
			mu.Unlock()
		}
	}()

	wg.Wait()
	return results
}

func (c *CompositeProvider) Resolve(track *core.Track) (string, error) {
	for _, p := range c.providers {
		if p.Name() == track.RemoteReference {
			streamURL, err := p.Resolve(track)
			if err == nil && streamURL != "" {
				return streamURL, nil
			}
		}
	}

	for _, p := range c.providers {
		streamURL, err := p.Resolve(track)
		if err == nil && streamURL != "" {
			return streamURL, nil
		}
	}

	return "", fmt.Errorf("failed to resolve audio stream for track: %s", track.Title)
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
