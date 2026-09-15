package providers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	monochromeURLRegex = regexp.MustCompile(`https?:\/\/(?:monochrome\.tf|(?:www\.)?tidal\.com\/(?:browse\/)?)(track|album|playlist|artist)\/([a-zA-Z0-9-]+)`)
)

type MonochromeProvider struct {
	client     *http.Client
	instances  []string
	qobuzPool  []string
	mu         sync.RWMutex
	currentIdx int
}

func NewMonochromeProvider() *MonochromeProvider {
	return &MonochromeProvider{
		client: &http.Client{Timeout: 10 * time.Second},
		instances: []string{
			"https://api.monochrome.tf",
			"https://eu-central.monochrome.tf",
			"https://us-west.monochrome.tf",
			"https://tidal-api.binimum.org",
			"https://triton.squid.wtf",
			"https://hifi.geeked.wtf",
		},
		qobuzPool: []string{
			"https://qobuz.kennyy.com.br",
			"https://trypt-hifi-dl-456461932686.us-west1.run.app",
		},
	}
}

func (m *MonochromeProvider) Name() string {
	return "Monochrome"
}

func (m *MonochromeProvider) getActiveInstance() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instances[m.currentIdx%len(m.instances)]
}

func (m *MonochromeProvider) rotateInstance() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentIdx = (m.currentIdx + 1) % len(m.instances)
}

type monochromeSearchResponse struct {
	Data struct {
		Tracks struct {
			Items []struct {
				ID         int64  `json:"id"`
				Title      string `json:"title"`
				Duration   int    `json:"duration"`
				Explicit   bool   `json:"explicit"`
				ISRC       string `json:"isrc"`
				Artist     struct {
					Name string `json:"name"`
				} `json:"artist"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Title string `json:"title"`
					Cover string `json:"cover"`
				} `json:"album"`
			} `json:"items"`
		} `json:"tracks"`
	} `json:"data"`
}

func (m *MonochromeProvider) Search(query string, limit int) ([]core.Track, error) {
	cleanQuery := strings.TrimSpace(query)
	cleanQuery = strings.TrimPrefix(cleanQuery, "mcsearch:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "tidal:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "mc:")
	cleanQuery = strings.TrimSpace(cleanQuery)

	if cleanQuery == "" {
		return nil, fmt.Errorf("empty query")
	}

	if limit <= 0 {
		limit = 10
	}

	if monochromeURLRegex.MatchString(cleanQuery) {
		t, err := m.ResolveURL(cleanQuery)
		if err == nil && len(t) > 0 {
			return t, nil
		}
	}

	for attempt := 0; attempt < len(m.instances); attempt++ {
		baseURL := m.getActiveInstance()
		searchURL := fmt.Sprintf("%s/search/?s=%s", baseURL, url.QueryEscape(cleanQuery))

		req, err := http.NewRequest(http.MethodGet, searchURL, nil)
		if err != nil {
			m.rotateInstance()
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Referer", "https://monochrome.tf/")

		resp, err := m.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			m.rotateInstance()
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var data monochromeSearchResponse
		err = json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if err != nil {
			m.rotateInstance()
			continue
		}

		var tracks []core.Track
		for _, item := range data.Data.Tracks.Items {
			if item.Title == "" {
				continue
			}

			artist := item.Artist.Name
			if artist == "" && len(item.Artists) > 0 {
				artist = item.Artists[0].Name
			}
			if artist == "" {
				artist = "Tidal Artist"
			}

			albumTitle := item.Album.Title
			if albumTitle == "" {
				albumTitle = "Tidal Album"
			}

			t := core.Track{
				ID:              uuid.New().String(),
				Title:           item.Title,
				Artist:          artist,
				Album:           albumTitle,
				Duration:        float64(item.Duration),
				Source:          core.SourceOnline,
				SourceID:        strconv.FormatInt(item.ID, 10),
				RemoteReference: m.Name(),
				DateAdded:       time.Now(),
			}
			tracks = append(tracks, t)
			if len(tracks) >= limit {
				break
			}
		}

		if len(tracks) > 0 {
			return tracks, nil
		}
	}

	return nil, fmt.Errorf("no results from monochrome/tidal proxy")
}

func (m *MonochromeProvider) ResolveURL(rawURL string) ([]core.Track, error) {
	match := monochromeURLRegex.FindStringSubmatch(rawURL)
	if len(match) < 3 {
		return nil, fmt.Errorf("invalid monochrome url: %s", rawURL)
	}

	id := match[2]
	for attempt := 0; attempt < len(m.instances); attempt++ {
		baseURL := m.getActiveInstance()
		trackURL := fmt.Sprintf("%s/track/?id=%s", baseURL, id)

		req, err := http.NewRequest(http.MethodGet, trackURL, nil)
		if err != nil {
			m.rotateInstance()
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Referer", "https://monochrome.tf/")

		resp, err := m.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			m.rotateInstance()
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var single struct {
			Data struct {
				ID       int64  `json:"id"`
				Title    string `json:"title"`
				Duration int    `json:"duration"`
				Artist   struct {
					Name string `json:"name"`
				} `json:"artist"`
				Album struct {
					Title string `json:"title"`
				} `json:"album"`
			} `json:"data"`
		}

		err = json.NewDecoder(resp.Body).Decode(&single)
		resp.Body.Close()
		if err != nil || single.Data.Title == "" {
			m.rotateInstance()
			continue
		}

		artist := single.Data.Artist.Name
		if artist == "" {
			artist = "Tidal Artist"
		}

		album := single.Data.Album.Title
		if album == "" {
			album = "Tidal Album"
		}

		t := core.Track{
			ID:              uuid.New().String(),
			Title:           single.Data.Title,
			Artist:          artist,
			Album:           album,
			Duration:        float64(single.Data.Duration),
			Source:          core.SourceOnline,
			SourceID:        strconv.FormatInt(single.Data.ID, 10),
			RemoteReference: m.Name(),
			DateAdded:       time.Now(),
		}
		return []core.Track{t}, nil
	}

	return nil, fmt.Errorf("unable to resolve track url on monochrome")
}

type monochromeManifestResponse struct {
	Data struct {
		Data struct {
			Attributes struct {
				URI      string `json:"uri"`
				Manifest string `json:"manifest"`
			} `json:"attributes"`
		} `json:"data"`
		Attributes struct {
			URI      string `json:"uri"`
			Manifest string `json:"manifest"`
		} `json:"attributes"`
		Manifest string `json:"manifest"`
	} `json:"data"`
	OriginalTrackURL string `json:"originalTrackUrl"`
}

func (m *MonochromeProvider) Resolve(track *core.Track) (string, error) {
	if track.SourceID == "" {
		return "", fmt.Errorf("missing source id")
	}

	for attempt := 0; attempt < len(m.instances); attempt++ {
		baseURL := m.getActiveInstance()
		manifestURL := fmt.Sprintf("%s/trackManifests/?id=%s&adaptive=true&manifestType=MPEG_DASH&uriScheme=HTTPS&usage=PLAYBACK&formats=FLAC&formats=AACLC&formats=HEAACV1",
			baseURL, track.SourceID)

		req, err := http.NewRequest(http.MethodGet, manifestURL, nil)
		if err != nil {
			m.rotateInstance()
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Referer", "https://monochrome.tf/")

		resp, err := m.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			m.rotateInstance()
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var res monochromeManifestResponse
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if err != nil {
			m.rotateInstance()
			continue
		}

		uri := res.Data.Data.Attributes.URI
		if uri == "" {
			uri = res.Data.Attributes.URI
		}
		if uri == "" {
			uri = res.OriginalTrackURL
		}
		if uri != "" {
			return uri, nil
		}

		manifestB64 := res.Data.Data.Attributes.Manifest
		if manifestB64 == "" {
			manifestB64 = res.Data.Attributes.Manifest
		}
		if manifestB64 == "" {
			manifestB64 = res.Data.Manifest
		}

		if manifestB64 != "" {
			decoded, dErr := base64.StdEncoding.DecodeString(manifestB64)
			if dErr == nil {
				var parsedManifest struct {
					URLs []string `json:"urls"`
				}
				if jErr := json.Unmarshal(decoded, &parsedManifest); jErr == nil && len(parsedManifest.URLs) > 0 {
					return parsedManifest.URLs[0], nil
				}
			}
		}
	}

	for _, qBase := range m.qobuzPool {
		query := fmt.Sprintf("%s %s", track.Title, track.Artist)
		searchURL := fmt.Sprintf("%s/api/get-music?q=%s&offset=0", qBase, url.QueryEscape(query))
		req, err := http.NewRequest(http.MethodGet, searchURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		resp, err := m.client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			var qSearch struct {
				Data struct {
					Tracks struct {
						Items []struct {
							ID int64 `json:"id"`
						} `json:"items"`
					} `json:"tracks"`
				} `json:"data"`
			}
			err = json.NewDecoder(resp.Body).Decode(&qSearch)
			resp.Body.Close()
			if err == nil && len(qSearch.Data.Tracks.Items) > 0 {
				qID := qSearch.Data.Tracks.Items[0].ID
				dlURL := fmt.Sprintf("%s/api/download-music?track_id=%d&quality=5", qBase, qID)
				dReq, dErr := http.NewRequest(http.MethodGet, dlURL, nil)
				if dErr == nil {
					dReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
					dResp, dErr := m.client.Do(dReq)
					if dErr == nil && dResp.StatusCode == http.StatusOK {
						var dRes struct {
							Data struct {
								URL string `json:"url"`
							} `json:"data"`
						}
						err = json.NewDecoder(dResp.Body).Decode(&dRes)
						dResp.Body.Close()
						if err == nil && dRes.Data.URL != "" {
							return dRes.Data.URL, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("failed to resolve audio stream from monochrome for track: %s", track.Title)
}
