package providers

import (
	"crypto/des"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

const (
	JioSaavnAPIBase = "https://www.jiosaavn.com/api.php"
	JioSaavnKey     = "38346591"
)

var (
	jioSaavnURLRegex = regexp.MustCompile(`https?:\/\/(?:www\.)?jiosaavn\.com\/(?:(album|featured|song|s\/playlist|artist)\/)(?:[^\/]+\/)([A-Za-z0-9_,-]+)`)
)

type JioSaavnProvider struct {
	client *http.Client
}

func NewJioSaavnProvider() *JioSaavnProvider {
	return &JioSaavnProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (j *JioSaavnProvider) Name() string {
	return "JioSaavn"
}

type jioSaavnSearchResponse struct {
	Total   int               `json:"total"`
	Start   int               `json:"start"`
	Results []jioSaavnSongRaw `json:"results"`
}

type jioSaavnWebApiGetResponse struct {
	Title    string            `json:"title"`
	Name     string            `json:"name"`
	List     []jioSaavnSongRaw `json:"list"`
	TopSongs []jioSaavnSongRaw `json:"topSongs"`
	Songs    []jioSaavnSongRaw `json:"songs"`
}

type jioSaavnSongRaw struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Song     string `json:"song"`
	Subtitle string `json:"subtitle"`
	Header   string `json:"header_desc"`
	Image    string `json:"image"`
	Year     string `json:"year"`
	PermaURL string `json:"perma_url"`
	MoreInfo struct {
		Album             string `json:"album"`
		Duration          string `json:"duration"`
		EncryptedMediaURL string `json:"encrypted_media_url"`
		MediaPreviewURL   string `json:"media_preview_url"`
		PrimaryArtists    string `json:"primary_artists"`
		Singers           string `json:"singers"`
	} `json:"more_info"`
	PrimaryArtists    string `json:"primary_artists"`
	Singers           string `json:"singers"`
	Album             string `json:"album"`
	Duration          string `json:"duration"`
	EncryptedMediaURL string `json:"encrypted_media_url"`
	MediaPreviewURL   string `json:"media_preview_url"`
}

func (j *JioSaavnProvider) Search(query string, limit int) ([]core.Track, error) {
	if limit <= 0 {
		limit = 10
	}

	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimPrefix(trimmed, "jssearch:")
	trimmed = strings.TrimPrefix(trimmed, "js:")
	trimmed = strings.TrimSpace(trimmed)

	if matches := jioSaavnURLRegex.FindStringSubmatch(trimmed); len(matches) > 2 {
		resType := matches[1]
		resID := matches[2]
		return j.resolveURL(resType, resID, limit)
	}

	searchURL := fmt.Sprintf(
		"%s?__call=search.getResults&_format=json&_marker=0&api_version=4&ctx=web6dot0&q=%s&n=%d",
		JioSaavnAPIBase, url.QueryEscape(trimmed), limit,
	)

	req, err := http.NewRequest(http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JioSaavn search failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data jioSaavnSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JioSaavn search json: %w", err)
	}

	var tracks []core.Track
	for _, item := range data.Results {
		t := j.parseTrack(item)
		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (j *JioSaavnProvider) resolveURL(resType, token string, limit int) ([]core.Track, error) {
	if resType == "song" {
		t, err := j.fetchSongDetails(token)
		if err != nil {
			return nil, err
		}
		return []core.Track{*t}, nil
	}

	apiType := resType
	if apiType == "featured" || apiType == "s/playlist" {
		apiType = "playlist"
	}

	apiURL := fmt.Sprintf(
		"%s?__call=webapi.get&api_version=4&token=%s&type=%s&n=%d",
		JioSaavnAPIBase, url.QueryEscape(token), apiType, limit,
	)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var listData jioSaavnWebApiGetResponse
	if err := json.Unmarshal(body, &listData); err != nil {
		return nil, fmt.Errorf("failed to parse JioSaavn list json: %w", err)
	}

	items := listData.List
	if len(items) == 0 {
		items = listData.TopSongs
	}
	if len(items) == 0 {
		items = listData.Songs
	}

	var tracks []core.Track
	for _, raw := range items {
		tracks = append(tracks, j.parseTrack(raw))
	}

	return tracks, nil
}

func (j *JioSaavnProvider) fetchSongDetails(songID string) (*core.Track, error) {
	detailsURL := fmt.Sprintf("%s?__call=song.getDetails&pids=%s&_format=json", JioSaavnAPIBase, url.QueryEscape(songID))
	req, err := http.NewRequest(http.MethodGet, detailsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var detailsMap map[string]jioSaavnSongRaw
	if err := json.Unmarshal(body, &detailsMap); err == nil {
		for _, s := range detailsMap {
			t := j.parseTrack(s)
			return &t, nil
		}
	}

	return nil, fmt.Errorf("song details not found for ID: %s", songID)
}

func (j *JioSaavnProvider) parseTrack(item jioSaavnSongRaw) core.Track {
	title := cleanHTML(item.Title)
	if title == "" {
		title = cleanHTML(item.Song)
	}

	artist := cleanHTML(item.Subtitle)
	if artist == "" {
		artist = cleanHTML(item.MoreInfo.PrimaryArtists)
	}
	if artist == "" {
		artist = cleanHTML(item.PrimaryArtists)
	}
	if artist == "" {
		artist = cleanHTML(item.MoreInfo.Singers)
	}
	if artist == "" {
		artist = "Unknown Artist"
	}

	album := cleanHTML(item.MoreInfo.Album)
	if album == "" {
		album = cleanHTML(item.Album)
	}

	artwork := item.Image
	artwork = strings.Replace(artwork, "150x150", "500x500", 1)
	artwork = strings.Replace(artwork, "50x50", "500x500", 1)

	durStr := item.MoreInfo.Duration
	if durStr == "" {
		durStr = item.Duration
	}
	durSec, _ := strconv.ParseFloat(durStr, 64)
	year, _ := strconv.Atoi(item.Year)

	track := core.Track{
		ID:              uuid.New().String(),
		Title:           title,
		Artist:          artist,
		Album:           album,
		Duration:        durSec,
		ArtworkURL:      artwork,
		Year:            year,
		Source:          core.SourceOnline,
		SourceID:        item.ID,
		RemoteReference: j.Name(),
		DateAdded:       time.Now(),
	}

	encURL := item.MoreInfo.EncryptedMediaURL
	if encURL == "" {
		encURL = item.EncryptedMediaURL
	}
	if encURL != "" {
		if streamURL, err := DecryptJioSaavnMediaURL(encURL); err == nil {
			track.StreamURL = UpgradeJioSaavnQuality(streamURL)
		}
	}

	previewURL := item.MoreInfo.MediaPreviewURL
	if previewURL == "" {
		previewURL = item.MediaPreviewURL
	}
	if track.StreamURL == "" && previewURL != "" {
		track.StreamURL = previewURL
	}

	return track
}

func (j *JioSaavnProvider) Resolve(track *core.Track) (string, error) {
	if track.StreamURL != "" {
		return track.StreamURL, nil
	}

	t, err := j.fetchSongDetails(track.SourceID)
	if err == nil && t.StreamURL != "" {
		return t.StreamURL, nil
	}

	return "", fmt.Errorf("could not resolve JioSaavn audio stream for: %s", track.Title)
}

func DecryptJioSaavnMediaURL(encryptedBase64 string) (string, error) {
	key := []byte(JioSaavnKey)
	block, err := des.NewCipher(key)
	if err != nil {
		return "", err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", err
	}

	if len(ciphertext)%des.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length is not a multiple of DES block size")
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += des.BlockSize {
		block.Decrypt(plaintext[i:i+des.BlockSize], ciphertext[i:i+des.BlockSize])
	}

	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty decrypted payload")
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding > des.BlockSize || padding > len(plaintext) {
		return string(plaintext), nil
	}
	for i := len(plaintext) - padding; i < len(plaintext); i++ {
		if plaintext[i] != byte(padding) {
			return string(plaintext), nil
		}
	}
	plaintext = plaintext[:len(plaintext)-padding]
	return string(plaintext), nil
}

func UpgradeJioSaavnQuality(urlStr string) string {
	urlStr = strings.Replace(urlStr, "_96.mp4", "_320.mp4", 1)
	urlStr = strings.Replace(urlStr, "_160.mp4", "_320.mp4", 1)
	urlStr = strings.Replace(urlStr, "_96.m4a", "_320.m4a", 1)
	urlStr = strings.Replace(urlStr, "_160.m4a", "_320.m4a", 1)
	return urlStr
}

func cleanHTML(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return strings.TrimSpace(s)
}
