package providers

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	gaanaURLRegex = regexp.MustCompile(`https?:\/\/(?:www\.)?gaana\.com\/(song|album|playlist|artist)\/([\w-]+)`)
	gaanaCryptoKey = []byte("gy1t#b@jl(b$wtme")
)

const (
	gaanaAPIBase    = "https://gaana.com/apiv2"
	gaanaStreamAPI  = "https://gaana.com/api/stream-url"
	gaanaHLSBase    = "https://vodhlsgaana-ebw.akamaized.net/"
)

type GaanaProvider struct {
	client   *http.Client
	fallback MusicProvider
}

func NewGaanaProvider(streamFallback MusicProvider) *GaanaProvider {
	return &GaanaProvider{
		client:   &http.Client{Timeout: 12 * time.Second},
		fallback: streamFallback,
	}
}

func (g *GaanaProvider) Name() string {
	return "Gaana"
}

type gaanaSearchResponse struct {
	Gr []struct {
		Ty string `json:"ty"`
		Gd []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Seo      string `json:"seo"`
			Sti      string `json:"sti"`
			Duration string `json:"duration"`
			Atw      string `json:"atw"`
		} `json:"gd"`
	} `json:"gr"`
}

type gaanaSongDetailResponse struct {
	Tracks []struct {
		TrackID      string      `json:"track_id"`
		TrackTitle   string      `json:"track_title"`
		Duration     interface{} `json:"duration"`
		Seokey       string      `json:"seokey"`
		ArtworkLarge string      `json:"artwork_large"`
		Artist       []struct {
			Name string `json:"name"`
		} `json:"artist"`
		AlbumTitle string `json:"album_title"`
	} `json:"tracks"`
}

func (g *GaanaProvider) Search(query string, limit int) ([]core.Track, error) {
	cleanQuery := strings.TrimSpace(query)
	cleanQuery = strings.TrimPrefix(cleanQuery, "gnsearch:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "gaanasearch:")
	cleanQuery = strings.TrimPrefix(cleanQuery, "gn:")
	cleanQuery = strings.TrimSpace(cleanQuery)

	if cleanQuery == "" {
		return nil, fmt.Errorf("empty query")
	}

	if limit <= 0 {
		limit = 10
	}

	if gaanaURLRegex.MatchString(cleanQuery) {
		t, err := g.ResolveURL(cleanQuery)
		if err == nil && len(t) > 0 {
			return t, nil
		}
	}

	searchURL := fmt.Sprintf("%s?country=IN&page=0&type=search&keyword=%s&secType=track",
		gaanaAPIBase, url.QueryEscape(cleanQuery))

	req, err := http.NewRequest(http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gaana search status: %d", resp.StatusCode)
	}

	var data gaanaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var tracks []core.Track
	for _, grp := range data.Gr {
		if strings.EqualFold(grp.Ty, "Track") {
			for _, item := range grp.Gd {
				durSec, _ := strconv.Atoi(item.Duration)
				title := item.Name
				if title == "" {
					title = item.Seo
				}
				artist := item.Sti
				if artist == "" {
					artist = "Gaana Artist"
				}

				t := core.Track{
					ID:              uuid.New().String(),
					Title:           title,
					Artist:          artist,
					Album:           "Gaana Track",
					Duration:        float64(durSec),
					ArtworkURL:      item.Atw,
					Source:          core.SourceOnline,
					SourceID:        item.ID,
					RemoteReference: g.Name(),
					DateAdded:       time.Now(),
				}
				tracks = append(tracks, t)
				if len(tracks) >= limit {
					break
				}
			}
			break
		}
	}

	return tracks, nil
}

func (g *GaanaProvider) ResolveURL(rawURL string) ([]core.Track, error) {
	match := gaanaURLRegex.FindStringSubmatch(rawURL)
	if len(match) < 3 {
		return nil, fmt.Errorf("invalid gaana url: %s", rawURL)
	}

	kind := match[1]
	seokey := match[2]

	detailURL := fmt.Sprintf("%s?type=%sDetail&seokey=%s", gaanaAPIBase, kind, seokey)
	req, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data gaanaSongDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var tracks []core.Track
	for _, trk := range data.Tracks {
		var artists []string
		for _, a := range trk.Artist {
			if a.Name != "" {
				artists = append(artists, a.Name)
			}
		}
		artistName := strings.Join(artists, ", ")
		if artistName == "" {
			artistName = "Gaana Artist"
		}

		album := trk.AlbumTitle
		if album == "" {
			album = "Gaana Release"
		}

		var durSec int
		switch d := trk.Duration.(type) {
		case float64:
			durSec = int(d)
		case string:
			durSec, _ = strconv.Atoi(d)
		}

		t := core.Track{
			ID:              uuid.New().String(),
			Title:           trk.TrackTitle,
			Artist:          artistName,
			Album:           album,
			Duration:        float64(durSec),
			ArtworkURL:      trk.ArtworkLarge,
			Source:          core.SourceOnline,
			SourceID:        trk.TrackID,
			RemoteReference: g.Name(),
			DateAdded:       time.Now(),
		}
		tracks = append(tracks, t)
	}

	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks found for gaana url")
	}

	return tracks, nil
}

type gaanaStreamResponse struct {
	APIStatus string `json:"api_status"`
	Data      struct {
		StreamPath string `json:"stream_path"`
	} `json:"data"`
}

func (g *GaanaProvider) decryptStreamPath(encryptedData string) string {
	if len(encryptedData) < 17 {
		return ""
	}

	offset, err := strconv.Atoi(string(encryptedData[0]))
	if err != nil || offset+16 >= len(encryptedData) {
		return ""
	}

	iv := []byte(encryptedData[offset : offset+16])
	ciphertextB64 := encryptedData[offset+16:] + "=="
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil || len(ciphertext)%aes.BlockSize != 0 {
		return ""
	}

	block, err := aes.NewCipher(gaanaCryptoKey)
	if err != nil {
		return ""
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(ciphertext))
	mode.CryptBlocks(decrypted, ciphertext)

	raw := string(decrypted)
	var printable strings.Builder
	for _, ch := range raw {
		if ch >= 32 && ch <= 126 {
			printable.WriteRune(ch)
		}
	}
	clean := printable.String()
	if idx := strings.Index(clean, "hls/"); idx != -1 {
		return gaanaHLSBase + clean[idx:]
	}

	return ""
}

func (g *GaanaProvider) Resolve(track *core.Track) (string, error) {
	if track.SourceID != "" {
		formData := url.Values{
			"quality":       {"high"},
			"track_id":      {track.SourceID},
			"stream_format": {"mp4"},
		}

		req, err := http.NewRequest(http.MethodPost, gaanaStreamAPI, strings.NewReader(formData.Encode()))
		if err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := g.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				var sRes gaanaStreamResponse
				if err := json.NewDecoder(resp.Body).Decode(&sRes); err == nil && sRes.APIStatus == "success" {
					hlsURL := g.decryptStreamPath(sRes.Data.StreamPath)
					if hlsURL != "" {
						return hlsURL, nil
					}
				}
			}
		}
	}

	if g.fallback != nil {
		query := fmt.Sprintf("%s %s", track.Title, track.Artist)
		results, err := g.fallback.Search(query, 5)
		if err == nil && len(results) > 0 {
			return g.fallback.Resolve(&results[0])
		}
	}

	return "", fmt.Errorf("unable to resolve stream for gaana track: %s", track.Title)
}
