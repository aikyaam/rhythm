package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	scTrackURLRegex  = regexp.MustCompile(`^https?:\/\/(?:www\.|m\.)?soundcloud\.com\/[^/\s]+\/(?:sets\/)?[^/\s?]+`)
	scShortURLRegex  = regexp.MustCompile(`^https?:\/\/(?:www\.)?on\.soundcloud\.com\/[A-Za-z0-9]+`)
	scClientIDRegex  = regexp.MustCompile(`(?:client_id[=:]\s*["']?|"clientId"\s*:\s*["']?)([a-zA-Z0-9]{32})`)
	scScriptSrcRegex = regexp.MustCompile(`https:\/\/a-v2\.sndcdn\.com\/assets\/[a-zA-Z0-9-]+\.js`)
)

const (
	scAPIBase        = "https://api-v2.soundcloud.com"
	scDefaultClient  = "Pb72ranhoyt6gw7hM7TkzUItXlMWSNSo"
)

type SoundCloudProvider struct {
	client      *http.Client
	mu          sync.RWMutex
	clientID    string
	clientValid time.Time
}

func NewSoundCloudProvider() *SoundCloudProvider {
	return &SoundCloudProvider{
		client:   &http.Client{Timeout: 15 * time.Second},
		clientID: scDefaultClient,
	}
}

func (s *SoundCloudProvider) Name() string {
	return "SoundCloud"
}

func (s *SoundCloudProvider) ensureClientID() string {
	s.mu.RLock()
	if s.clientID != "" && time.Now().Before(s.clientValid) {
		cid := s.clientID
		s.mu.RUnlock()
		return cid
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.clientID != "" && time.Now().Before(s.clientValid) {
		return s.clientID
	}

	req, err := http.NewRequest(http.MethodGet, "https://soundcloud.com", nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		resp, err := s.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			matches := scScriptSrcRegex.FindAllString(bodyStr, -1)
			for _, scriptURL := range matches {
				sReq, sErr := http.NewRequest(http.MethodGet, scriptURL, nil)
				if sErr == nil {
					sReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
					sResp, sErr := s.client.Do(sReq)
					if sErr == nil {
						sBody, _ := io.ReadAll(sResp.Body)
						sResp.Body.Close()
						idMatch := scClientIDRegex.FindSubmatch(sBody)
						if len(idMatch) > 1 {
							foundID := string(idMatch[1])
							if len(foundID) == 32 {
								s.clientID = foundID
								s.clientValid = time.Now().Add(6 * time.Hour)
								return s.clientID
							}
						}
					}
				}
			}
		}
	}

	if s.clientID == "" {
		s.clientID = scDefaultClient
	}
	s.clientValid = time.Now().Add(1 * time.Hour)
	return s.clientID
}

type scTrackItem struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Duration    int    `json:"duration"`
	Permalink   string `json:"permalink_url"`
	ArtworkURL  string `json:"artwork_url"`
	User        struct {
		Username string `json:"username"`
	} `json:"user"`
	Media struct {
		Transcodings []scTranscoding `json:"transcodings"`
	} `json:"media"`
}

type scTranscoding struct {
	URL      string `json:"url"`
	Preset   string `json:"preset"`
	Duration int    `json:"duration"`
	Format   struct {
		Protocol string `json:"protocol"`
		MimeType string `json:"mime_type"`
	} `json:"format"`
	Quality string `json:"quality"`
}

type scSearchResponse struct {
	Collection []scTrackItem `json:"collection"`
	Total      int           `json:"total_results"`
}

func (s *SoundCloudProvider) Search(query string, limit int) ([]core.Track, error) {
	cleanQuery := strings.TrimSpace(query)
	cleanQuery = strings.TrimPrefix(cleanQuery, "scsearch:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "sc:")
	cleanQuery = strings.TrimSpace(cleanQuery)

	if cleanQuery == "" {
		return nil, fmt.Errorf("empty query")
	}

	if limit <= 0 {
		limit = 10
	}

	if scTrackURLRegex.MatchString(cleanQuery) || scShortURLRegex.MatchString(cleanQuery) {
		t, err := s.ResolveURL(cleanQuery)
		if err == nil && t != nil {
			return []core.Track{*t}, nil
		}
	}

	cid := s.ensureClientID()
	endpoint := fmt.Sprintf("%s/search/tracks?q=%s&client_id=%s&limit=%d&offset=0",
		scAPIBase, url.QueryEscape(cleanQuery), cid, limit)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
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
		return nil, fmt.Errorf("soundcloud search status: %d", resp.StatusCode)
	}

	var sr scSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}

	var tracks []core.Track
	for _, item := range sr.Collection {
		if item.Title == "" {
			continue
		}

		author := item.User.Username
		if author == "" {
			author = "SoundCloud Creator"
		}

		art := item.ArtworkURL
		if art != "" {
			art = strings.Replace(art, "-large.jpg", "-t500x500.jpg", 1)
		}

		track := core.Track{
			ID:              uuid.New().String(),
			Title:           item.Title,
			Artist:          author,
			Album:           "SoundCloud Single",
			Duration:        float64(item.Duration) / 1000.0,
			ArtworkURL:      art,
			Source:          core.SourceOnline,
			SourceID:        fmt.Sprintf("%d", item.ID),
			RemoteReference: s.Name(),
			DateAdded:       time.Now(),
		}
		tracks = append(tracks, track)
	}

	return tracks, nil
}

func (s *SoundCloudProvider) ResolveURL(trackURL string) (*core.Track, error) {
	cid := s.ensureClientID()
	resolveAPI := fmt.Sprintf("%s/resolve?url=%s&client_id=%s", scAPIBase, url.QueryEscape(trackURL), cid)

	req, err := http.NewRequest(http.MethodGet, resolveAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve status: %d", resp.StatusCode)
	}

	var item scTrackItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}

	if item.Title == "" {
		return nil, fmt.Errorf("no track metadata resolved")
	}

	author := item.User.Username
	if author == "" {
		author = "SoundCloud Creator"
	}

	art := item.ArtworkURL
	if art != "" {
		art = strings.Replace(art, "-large.jpg", "-t500x500.jpg", 1)
	}

	t := &core.Track{
		ID:              uuid.New().String(),
		Title:           item.Title,
		Artist:          author,
		Album:           "SoundCloud",
		Duration:        float64(item.Duration) / 1000.0,
		ArtworkURL:      art,
		Source:          core.SourceOnline,
		SourceID:        fmt.Sprintf("%d", item.ID),
		RemoteReference: s.Name(),
		DateAdded:       time.Now(),
	}
	return t, nil
}

type scTranscodingStreamResult struct {
	URL string `json:"url"`
}

func (s *SoundCloudProvider) Resolve(track *core.Track) (string, error) {
	if track.SourceID == "" {
		return "", fmt.Errorf("missing track source id")
	}

	cid := s.ensureClientID()
	trackAPI := fmt.Sprintf("%s/tracks/%s?client_id=%s", scAPIBase, track.SourceID, cid)

	req, err := http.NewRequest(http.MethodGet, trackAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch track status: %d", resp.StatusCode)
	}

	var item scTrackItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return "", err
	}

	var chosenTranscoding *scTranscoding
	for _, tc := range item.Media.Transcodings {
		if tc.Format.Protocol == "progressive" && strings.Contains(tc.Format.MimeType, "mpeg") {
			chosenTranscoding = &tc
			break
		}
	}
	if chosenTranscoding == nil {
		for _, tc := range item.Media.Transcodings {
			if tc.Format.Protocol == "progressive" {
				chosenTranscoding = &tc
				break
			}
		}
	}
	if chosenTranscoding == nil {
		for _, tc := range item.Media.Transcodings {
			if tc.Format.Protocol == "hls" && strings.Contains(tc.Format.MimeType, "aac") {
				chosenTranscoding = &tc
				break
			}
		}
	}
	if chosenTranscoding == nil && len(item.Media.Transcodings) > 0 {
		chosenTranscoding = &item.Media.Transcodings[0]
	}

	if chosenTranscoding == nil {
		return "", fmt.Errorf("no available audio transcoding for track: %s", track.Title)
	}

	streamAuthURL := fmt.Sprintf("%s?client_id=%s", chosenTranscoding.URL, cid)
	sReq, err := http.NewRequest(http.MethodGet, streamAuthURL, nil)
	if err != nil {
		return "", err
	}
	sReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	sResp, err := s.client.Do(sReq)
	if err != nil {
		return "", err
	}
	defer sResp.Body.Close()

	if sResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcoding auth failed with status: %d", sResp.StatusCode)
	}

	var streamRes scTranscodingStreamResult
	if err := json.NewDecoder(sResp.Body).Decode(&streamRes); err != nil {
		return "", err
	}

	if streamRes.URL == "" {
		return "", fmt.Errorf("empty stream url returned from soundcloud")
	}

	return streamRes.URL, nil
}
