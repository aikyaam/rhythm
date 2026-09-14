package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	spotifyURLRegex      = regexp.MustCompile(`(?:https?:\/\/open\.spotify\.com\/(?:intl-[a-z]+\/)?(track|album|playlist)\/|spotify:(track|album|playlist):)([a-zA-Z0-9]+)`)
	spotifyNextDataRegex = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
)

type SpotifyProvider struct {
	client   *http.Client
	fallback MusicProvider
}

func NewSpotifyProvider(streamFallback MusicProvider) *SpotifyProvider {
	return &SpotifyProvider{
		client:   &http.Client{Timeout: 10 * time.Second},
		fallback: streamFallback,
	}
}

func (s *SpotifyProvider) Name() string {
	return "Spotify"
}

type spotifyNextData struct {
	Props struct {
		PageProps struct {
			State struct {
				Data struct {
					Entity struct {
						Name    string `json:"name"`
						Title   string `json:"title"`
						Artists []struct {
							Name string `json:"name"`
						} `json:"artists"`
						ReleaseDate struct {
							IsoString string `json:"isoString"`
						} `json:"releaseDate"`
						Duration     float64 `json:"duration"`
						AudioPreview struct {
							URL string `json:"url"`
						} `json:"audioPreview"`
						VisualIdentity struct {
							Image []struct {
								URL       string `json:"url"`
								MaxHeight int    `json:"maxHeight"`
							} `json:"image"`
						} `json:"visualIdentity"`
						TrackList []struct {
							URI      string  `json:"uri"`
							Title    string  `json:"title"`
							Subtitle string  `json:"subtitle"`
							Duration float64 `json:"duration"`
						} `json:"trackList"`
					} `json:"entity"`
				} `json:"data"`
			} `json:"state"`
		} `json:"pageProps"`
	} `json:"props"`
}

type spotifyOEmbedResponse struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ThumbnailURL string `json:"thumbnail_url"`
}

func (s *SpotifyProvider) Search(query string, limit int) ([]core.Track, error) {
	trimmed := strings.TrimSpace(query)

	if matches := spotifyURLRegex.FindStringSubmatch(trimmed); len(matches) > 3 {
		resType := matches[1]
		if resType == "" {
			resType = matches[2]
		}
		resID := matches[3]

		switch resType {
		case "track":
			track, err := s.fetchTrackMetadata(resID)
			if err != nil {
				return nil, err
			}
			return []core.Track{*track}, nil
		case "album", "playlist":
			return s.fetchListMetadata(resType, resID, limit)
		}
	}

	searchQuery := trimmed
	if strings.HasPrefix(searchQuery, "spsearch:") {
		searchQuery = strings.TrimPrefix(searchQuery, "spsearch:")
	}

	if s.fallback != nil {
		results, err := s.fallback.Search(searchQuery, limit)
		if err == nil {
			for i := range results {
				results[i].RemoteReference = s.Name()
			}
			return results, nil
		}
	}

	return nil, fmt.Errorf("spotify search could not find tracks for: %s", query)
}

func (s *SpotifyProvider) fetchTrackMetadata(trackID string) (*core.Track, error) {
	embedURL := fmt.Sprintf("https://open.spotify.com/embed/track/%s", trackID)
	req, err := http.NewRequest(http.MethodGet, embedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			if matches := spotifyNextDataRegex.FindSubmatch(body); len(matches) > 1 {
				var data spotifyNextData
				if err := json.Unmarshal(matches[1], &data); err == nil {
					entity := data.Props.PageProps.State.Data.Entity
					title := entity.Name
					if title == "" {
						title = entity.Title
					}
					artist := "Unknown Artist"
					if len(entity.Artists) > 0 {
						artist = entity.Artists[0].Name
					}

					artwork := ""
					for _, img := range entity.VisualIdentity.Image {
						artwork = img.URL
					}

					year := 0
					if len(entity.ReleaseDate.IsoString) >= 4 {
						fmt.Sscanf(entity.ReleaseDate.IsoString[:4], "%d", &year)
					}

					track := &core.Track{
						ID:              uuid.New().String(),
						Title:           title,
						Artist:          artist,
						Album:           "Spotify Release",
						Duration:        entity.Duration / 1000.0,
						ArtworkURL:      artwork,
						Year:            year,
						Source:          core.SourceOnline,
						SourceID:        trackID,
						RemoteReference: s.Name(),
						StreamURL:       "",
						DateAdded:       time.Now(),
					}
					return track, nil
				}
			}
		}
	}

	oembedURL := fmt.Sprintf("https://open.spotify.com/oembed?url=https://open.spotify.com/track/%s", trackID)
	oReq, err := http.NewRequest(http.MethodGet, oembedURL, nil)
	if err != nil {
		return nil, err
	}
	oReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	oResp, err := s.client.Do(oReq)
	if err != nil {
		return nil, err
	}
	defer oResp.Body.Close()

	if oResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spotify metadata returned status %d", oResp.StatusCode)
	}

	var data spotifyOEmbedResponse
	if err := json.NewDecoder(oResp.Body).Decode(&data); err != nil {
		return nil, err
	}

	title := data.Title
	artist := data.AuthorName
	if artist == "" && strings.Contains(title, " - ") {
		parts := strings.SplitN(title, " - ", 2)
		artist = strings.TrimSpace(parts[0])
		title = strings.TrimSpace(parts[1])
	}

	track := &core.Track{
		ID:              uuid.New().String(),
		Title:           title,
		Artist:          artist,
		Album:           "Spotify Release",
		ArtworkURL:      data.ThumbnailURL,
		Source:          core.SourceOnline,
		SourceID:        trackID,
		RemoteReference: s.Name(),
		DateAdded:       time.Now(),
	}

	return track, nil
}

func (s *SpotifyProvider) fetchListMetadata(resType, resID string, limit int) ([]core.Track, error) {
	embedURL := fmt.Sprintf("https://open.spotify.com/embed/%s/%s", resType, resID)
	req, err := http.NewRequest(http.MethodGet, embedURL, nil)
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
		return nil, fmt.Errorf("failed to fetch spotify %s (status %d)", resType, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	matches := spotifyNextDataRegex.FindSubmatch(body)
	if len(matches) <= 1 {
		return nil, fmt.Errorf("could not extract metadata from spotify embed")
	}

	var data spotifyNextData
	if err := json.Unmarshal(matches[1], &data); err != nil {
		return nil, fmt.Errorf("failed to parse spotify embed json: %w", err)
	}

	entity := data.Props.PageProps.State.Data.Entity
	albumName := entity.Name
	if albumName == "" {
		albumName = entity.Title
	}

	artwork := ""
	for _, img := range entity.VisualIdentity.Image {
		artwork = img.URL
	}

	var tracks []core.Track
	for i, t := range entity.TrackList {
		if limit > 0 && i >= limit {
			break
		}

		parts := strings.Split(t.URI, ":")
		trackID := t.URI
		if len(parts) == 3 {
			trackID = parts[2]
		}

		track := core.Track{
			ID:              uuid.New().String(),
			Title:           t.Title,
			Artist:          t.Subtitle,
			Album:           albumName,
			Duration:        t.Duration / 1000.0,
			ArtworkURL:      artwork,
			Source:          core.SourceOnline,
			SourceID:        trackID,
			RemoteReference: s.Name(),
			DateAdded:       time.Now(),
		}
		tracks = append(tracks, track)
	}

	return tracks, nil
}

func (s *SpotifyProvider) Resolve(track *core.Track) (string, error) {
	if track.StreamURL != "" {
		return track.StreamURL, nil
	}

	if s.fallback != nil {
		query := fmt.Sprintf("%s %s", track.Artist, track.Title)
		candidates, err := s.fallback.Search(query, 3)
		if err == nil && len(candidates) > 0 {
			return s.fallback.Resolve(&candidates[0])
		}
	}

	return "", fmt.Errorf("failed to resolve audio stream for Spotify track: %s - %s", track.Artist, track.Title)
}
