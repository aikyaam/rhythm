package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	songLinkURLRegex = regexp.MustCompile(`https?:\/\/(?:www\.)?(song\.link|album\.link|artist\.link|pods\.link|odesli\.co)\/.+`)
)

type SongLinkProvider struct {
	client   *http.Client
	fallback MusicProvider
}

func NewSongLinkProvider(streamFallback MusicProvider) *SongLinkProvider {
	return &SongLinkProvider{
		client:   &http.Client{Timeout: 12 * time.Second},
		fallback: streamFallback,
	}
}

func (s *SongLinkProvider) Name() string {
	return "SongLink"
}

type songLinkResponse struct {
	EntityUniqueID string `json:"entityUniqueId"`
	UserCountry    string `json:"userCountry"`
	PageURL        string `json:"pageUrl"`
	EntitiesByUniqueID map[string]struct {
		ID           string  `json:"id"`
		Type         string  `json:"type"`
		Title        string  `json:"title"`
		ArtistName   string  `json:"artistName"`
		ThumbnailURL string  `json:"thumbnailUrl"`
		Duration     float64 `json:"duration"`
	} `json:"entitiesByUniqueId"`
	LinksByPlatform map[string]struct {
		URL            string `json:"url"`
		EntityUniqueID string `json:"entityUniqueId"`
	} `json:"linksByPlatform"`
}

func (s *SongLinkProvider) Search(query string, limit int) ([]core.Track, error) {
	cleanQuery := strings.TrimSpace(query)
	cleanQuery = strings.TrimPrefix(cleanQuery, "slsearch:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "songlink:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "odesli:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "sl:")
	cleanQuery = strings.TrimSpace(cleanQuery)

	if cleanQuery == "" {
		return nil, fmt.Errorf("empty query")
	}

	if songLinkURLRegex.MatchString(cleanQuery) || strings.Contains(cleanQuery, "http") {
		return s.ResolveURL(cleanQuery)
	}

	if s.fallback != nil {
		return s.fallback.Search(cleanQuery, limit)
	}

	return nil, fmt.Errorf("songlink search requires URL or fallback provider")
}

func (s *SongLinkProvider) ResolveURL(linkURL string) ([]core.Track, error) {
	apiURL := fmt.Sprintf("https://api.song.link/v1-alpha.1/links?url=%s&userCountry=US&songIfSingle=true",
		url.QueryEscape(linkURL))

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("songlink api status: %d", resp.StatusCode)
	}

	var data songLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	entity, exists := data.EntitiesByUniqueID[data.EntityUniqueID]
	if !exists {
		for _, e := range data.EntitiesByUniqueID {
			entity = e
			exists = true
			break
		}
	}

	if !exists || entity.Title == "" {
		return nil, fmt.Errorf("no track metadata found in songlink response")
	}

	artist := entity.ArtistName
	if artist == "" {
		artist = "SongLink Artist"
	}

	var bestPlayableURL string
	platformOrder := []string{"youtube", "youtubeMusic", "spotify", "soundcloud", "appleMusic"}
	for _, p := range platformOrder {
		if pl, ok := data.LinksByPlatform[p]; ok && pl.URL != "" {
			bestPlayableURL = pl.URL
			break
		}
	}

	t := core.Track{
		ID:              uuid.New().String(),
		Title:           entity.Title,
		Artist:          artist,
		Album:           "Odesli Universal Link",
		Duration:        entity.Duration,
		ArtworkURL:      entity.ThumbnailURL,
		Source:          core.SourceOnline,
		SourceID:        entity.ID,
		RemoteReference: s.Name(),
		DateAdded:       time.Now(),
	}

	if bestPlayableURL != "" {
		t.SourceID = bestPlayableURL
	}

	return []core.Track{t}, nil
}

func (s *SongLinkProvider) Resolve(track *core.Track) (string, error) {
	if s.fallback != nil {
		if strings.HasPrefix(track.SourceID, "http") {
			streamURL, err := s.fallback.Resolve(&core.Track{
				Title:    track.Title,
				Artist:   track.Artist,
				SourceID: track.SourceID,
			})
			if err == nil && streamURL != "" {
				return streamURL, nil
			}
		}

		query := fmt.Sprintf("%s %s", track.Title, track.Artist)
		results, err := s.fallback.Search(query, 5)
		if err == nil && len(results) > 0 {
			return s.fallback.Resolve(&results[0])
		}
	}

	return "", fmt.Errorf("failed to resolve audio stream for songlink track: %s", track.Title)
}
