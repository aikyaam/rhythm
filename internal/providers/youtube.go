package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

var (
	ytVideoIDRegex = regexp.MustCompile(`(?:youtu\.be\/|youtube\.com\/(?:embed\/|v\/|watch\?v=|watch\?.+&v=|shorts\/))([\w-]{11})`)
)

const (
	ytVRUserAgent     = "Mozilla/5.0 (X11; Linux x86_64; Quest 3) AppleWebKit/537.36 (KHTML, like Gecko) OculusBrowser/39.3.0.11.46.766180192 Chrome/136.0.7103.177 VR Safari/537.36,gzip(gfe);GoogleHypersonic"
	ytVRClientName    = "ANDROID_VR"
	ytVRClientVersion = "1.65.10"
	ytVRDeviceMake    = "Google"
	ytVROSName        = "Android"
	ytVROSVersion     = "15"

	ytIOSUserAgent     = "com.google.ios.youtube/21.02.1 (iPhone16,2; U; CPU iOS 18_2 like Mac OS X;)"
	ytIOSClientName    = "IOS"
	ytIOSClientVersion = "21.02.1"
	ytIOSDeviceMake    = "Apple"
	ytIOSDeviceModel   = "iPhone16,2"
	ytIOSOSName        = "iPhone"
	ytIOSOSVersion     = "18.2.22C152"
)

type YouTubeProvider struct {
	client       *http.Client
	mu           sync.RWMutex
	visitorData  string
	visitorUntil time.Time
}

func NewYouTubeProvider() *YouTubeProvider {
	return &YouTubeProvider{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (y *YouTubeProvider) Name() string {
	return "YouTube"
}

func (y *YouTubeProvider) ensureVisitorData() string {
	y.mu.RLock()
	if y.visitorData != "" && time.Now().Before(y.visitorUntil) {
		vd := y.visitorData
		y.mu.RUnlock()
		return vd
	}
	y.mu.RUnlock()

	y.mu.Lock()
	defer y.mu.Unlock()

	if y.visitorData != "" && time.Now().Before(y.visitorUntil) {
		return y.visitorData
	}

	req, err := http.NewRequest(http.MethodGet, "https://www.youtube.com/embed", nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		req.Header.Set("Cookie", "YSC=cz5kYp3ZuIE; VISITOR_INFO1_LIVE=U-0T5oUyzf8;")
		resp, err := y.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			idx := bytes.Index(b, []byte("\"VISITOR_DATA\":\""))
			if idx != -1 {
				rest := b[idx+len("\"VISITOR_DATA\":\""):]
				end := bytes.IndexByte(rest, '"')
				if end != -1 {
					y.visitorData = string(rest[:end])
					y.visitorUntil = time.Now().Add(1 * time.Hour)
					return y.visitorData
				}
			}
		}
	}

	return y.visitorData
}

func (y *YouTubeProvider) buildVRContext(visitorData string) map[string]interface{} {
	clientMap := map[string]interface{}{
		"clientName":    ytVRClientName,
		"clientVersion": ytVRClientVersion,
		"deviceMake":    ytVRDeviceMake,
		"osName":        ytVROSName,
		"osVersion":     ytVROSVersion,
		"hl":            "en",
		"gl":            "US",
	}
	if visitorData != "" {
		clientMap["visitorData"] = visitorData
	}

	return map[string]interface{}{
		"client":  clientMap,
		"user":    map[string]interface{}{"lockedSafetyMode": false},
		"request": map[string]interface{}{"useSsl": true},
	}
}

func (y *YouTubeProvider) buildIOSContext(visitorData string) map[string]interface{} {
	clientMap := map[string]interface{}{
		"clientName":    ytIOSClientName,
		"clientVersion": ytIOSClientVersion,
		"deviceMake":    ytIOSDeviceMake,
		"deviceModel":   ytIOSDeviceModel,
		"osName":        ytIOSOSName,
		"osVersion":     ytIOSOSVersion,
		"hl":            "en",
		"gl":            "US",
	}
	if visitorData != "" {
		clientMap["visitorData"] = visitorData
	}

	return map[string]interface{}{
		"client":  clientMap,
		"user":    map[string]interface{}{"lockedSafetyMode": false},
		"request": map[string]interface{}{"useSsl": true},
	}
}

func (y *YouTubeProvider) Search(query string, limit int) ([]core.Track, error) {
	if limit <= 0 {
		limit = 10
	}

	searchQuery := strings.TrimSpace(query)
	if strings.HasPrefix(searchQuery, "ytsearch:") {
		searchQuery = strings.TrimSpace(strings.TrimPrefix(searchQuery, "ytsearch:"))
	}

	if matches := ytVideoIDRegex.FindStringSubmatch(searchQuery); len(matches) > 1 {
		videoID := matches[1]
		track, err := y.fetchVideoMetadata(videoID)
		if err == nil && track != nil {
			return []core.Track{*track}, nil
		}
	}

	visitorData := y.ensureVisitorData()

	tracks, err := y.searchWithContext(searchQuery, limit, y.buildVRContext(visitorData), ytVRUserAgent, visitorData)
	if err == nil && len(tracks) > 0 {
		return tracks, nil
	}

	tracksIOS, errIOS := y.searchWithContext(searchQuery, limit, y.buildIOSContext(visitorData), ytIOSUserAgent, visitorData)
	if errIOS == nil && len(tracksIOS) > 0 {
		return tracksIOS, nil
	}

	if err != nil {
		return nil, err
	}
	return tracks, nil
}

func (y *YouTubeProvider) searchWithContext(searchQuery string, limit int, clientCtx map[string]interface{}, userAgent, visitorData string) ([]core.Track, error) {
	body := map[string]interface{}{
		"context": clientCtx,
		"query":   searchQuery,
		"params":  "EgIQAQ%3D%3D",
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode YouTube search body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://youtubei.googleapis.com/youtubei/v1/search", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if visitorData != "" {
		req.Header.Set("X-Goog-Visitor-Id", visitorData)
	}

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("YouTube search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube search returned status %d", resp.StatusCode)
	}

	var searchResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse YouTube search response: %w", err)
	}

	tracks := y.parseSearchResults(searchResp, limit)
	return tracks, nil
}

func (y *YouTubeProvider) parseSearchResults(data map[string]interface{}, limit int) []core.Track {
	contents, _ := data["contents"].(map[string]interface{})
	slr, _ := contents["sectionListRenderer"].(map[string]interface{})
	slrContents, _ := slr["contents"].([]interface{})
	if len(slrContents) == 0 {
		return nil
	}

	lastIdx := len(slrContents) - 1
	itemSec, _ := slrContents[lastIdx].(map[string]interface{})
	isr, _ := itemSec["itemSectionRenderer"].(map[string]interface{})
	items, _ := isr["contents"].([]interface{})

	var tracks []core.Track
	for _, it := range items {
		if limit > 0 && len(tracks) >= limit {
			break
		}
		itemMap, ok := it.(map[string]interface{})
		if !ok {
			continue
		}

		renderer, ok := itemMap["compactVideoRenderer"].(map[string]interface{})
		if !ok {
			renderer, ok = itemMap["videoRenderer"].(map[string]interface{})
		}
		if !ok {
			continue
		}

		videoID, _ := renderer["videoId"].(string)
		if videoID == "" {
			continue
		}

		title := extractText(renderer["title"])
		if title == "" {
			title = "Unknown Title"
		}

		author := extractText(renderer["shortBylineText"])
		if author == "" {
			author = extractText(renderer["longBylineText"])
		}
		if author == "" {
			author = extractText(renderer["ownerText"])
		}
		if author == "" {
			author = "Unknown Artist"
		}

		if author == "Unknown Artist" && strings.Contains(title, " - ") {
			parts := strings.SplitN(title, " - ", 2)
			author = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}

		durStr := extractText(renderer["lengthText"])
		durSec := parseDurationToSeconds(durStr)

		artwork := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
		if thumbs, ok := renderer["thumbnail"].(map[string]interface{}); ok {
			if list, ok := thumbs["thumbnails"].([]interface{}); ok && len(list) > 0 {
				lastThumb := list[len(list)-1].(map[string]interface{})
				if u, ok := lastThumb["url"].(string); ok && u != "" {
					artwork = u
				}
			}
		}

		track := core.Track{
			ID:              uuid.New().String(),
			Title:           title,
			Artist:          author,
			Album:           "YouTube",
			Duration:        durSec,
			ArtworkURL:      artwork,
			Source:          core.SourceOnline,
			SourceID:        videoID,
			RemoteReference: y.Name(),
			DateAdded:       time.Now(),
		}
		tracks = append(tracks, track)
	}

	return tracks
}

func (y *YouTubeProvider) fetchVideoMetadata(videoID string) (*core.Track, error) {
	visitorData := y.ensureVisitorData()

	track, err := y.fetchPlayerWithContext(videoID, y.buildVRContext(visitorData), ytVRUserAgent, visitorData)
	if err == nil && track != nil {
		return track, nil
	}

	return y.fetchPlayerWithContext(videoID, y.buildIOSContext(visitorData), ytIOSUserAgent, visitorData)
}

func (y *YouTubeProvider) fetchPlayerWithContext(videoID string, clientCtx map[string]interface{}, userAgent, visitorData string) (*core.Track, error) {
	body := map[string]interface{}{
		"context":        clientCtx,
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, "https://youtubei.googleapis.com/youtubei/v1/player?prettyPrint=false", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if visitorData != "" {
		req.Header.Set("X-Goog-Visitor-Id", visitorData)
	}

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("player endpoint status %d", resp.StatusCode)
	}

	var playerResp ytPlayerResponse
	if err := json.NewDecoder(resp.Body).Decode(&playerResp); err != nil {
		return nil, err
	}

	durSec, _ := strconv.ParseFloat(playerResp.VideoDetails.LengthSeconds, 64)
	author := playerResp.VideoDetails.Author
	title := playerResp.VideoDetails.Title
	if author == "" {
		author = "Unknown Artist"
	}
	if strings.Contains(title, " - ") {
		parts := strings.SplitN(title, " - ", 2)
		author = strings.TrimSpace(parts[0])
		title = strings.TrimSpace(parts[1])
	}

	artwork := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
	if len(playerResp.VideoDetails.Thumbnail.Thumbnails) > 0 {
		artwork = playerResp.VideoDetails.Thumbnail.Thumbnails[len(playerResp.VideoDetails.Thumbnail.Thumbnails)-1].URL
	}

	track := &core.Track{
		ID:              uuid.New().String(),
		Title:           title,
		Artist:          author,
		Album:           "YouTube",
		Duration:        durSec,
		ArtworkURL:      artwork,
		Source:          core.SourceOnline,
		SourceID:        videoID,
		RemoteReference: y.Name(),
		DateAdded:       time.Now(),
	}

	if streamURL := y.extractBestAudioURL(&playerResp); streamURL != "" {
		track.StreamURL = streamURL
	}

	return track, nil
}

type ytPlayerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	VideoDetails struct {
		VideoID       string `json:"videoId"`
		Title         string `json:"title"`
		LengthSeconds string `json:"lengthSeconds"`
		Author        string `json:"author"`
		ChannelID     string `json:"channelId"`
		Thumbnail     struct {
			Thumbnails []struct {
				URL string `json:"url"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"videoDetails"`
	StreamingData struct {
		Formats         []ytStreamFormat `json:"formats"`
		AdaptiveFormats []ytStreamFormat `json:"adaptiveFormats"`
	} `json:"streamingData"`
}

type ytStreamFormat struct {
	Itag            int    `json:"itag"`
	MimeType        string `json:"mimeType"`
	Bitrate         int    `json:"bitrate"`
	AudioQuality    string `json:"audioQuality"`
	URL             string `json:"url"`
	SignatureCipher string `json:"signatureCipher"`
	ContentLength   string `json:"contentLength"`
}

func (y *YouTubeProvider) extractBestAudioURL(playerResp *ytPlayerResponse) string {
	var bestAudio ytStreamFormat
	highestBitrate := 0

	for _, f := range playerResp.StreamingData.AdaptiveFormats {
		if strings.Contains(f.MimeType, "audio/mp4") && f.URL != "" {
			if f.Bitrate > highestBitrate {
				highestBitrate = f.Bitrate
				bestAudio = f
			}
		}
	}

	if bestAudio.URL != "" {
		return bestAudio.URL
	}

	for _, f := range playerResp.StreamingData.AdaptiveFormats {
		if strings.HasPrefix(f.MimeType, "audio/") && f.URL != "" {
			if f.Bitrate > highestBitrate {
				highestBitrate = f.Bitrate
				bestAudio = f
			}
		}
	}

	if bestAudio.URL != "" {
		return bestAudio.URL
	}

	for _, f := range playerResp.StreamingData.Formats {
		if f.URL != "" && f.Bitrate > highestBitrate {
			highestBitrate = f.Bitrate
			bestAudio = f
		}
	}

	return bestAudio.URL
}

func (y *YouTubeProvider) ResolveRaw(track *core.Track) (string, error) {
	videoID := track.SourceID
	if matches := ytVideoIDRegex.FindStringSubmatch(videoID); len(matches) > 1 {
		videoID = matches[1]
	}

	visitorData := y.ensureVisitorData()

	streamURL, err := y.requestPlayerStream(videoID, y.buildVRContext(visitorData), ytVRUserAgent, visitorData)
	if err == nil && streamURL != "" {
		return streamURL, nil
	}

	streamURL, errIOS := y.requestPlayerStream(videoID, y.buildIOSContext(visitorData), ytIOSUserAgent, visitorData)
	if errIOS == nil && streamURL != "" {
		return streamURL, nil
	}

	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("no direct playable audio stream found for video: %s", videoID)
}

func (y *YouTubeProvider) requestPlayerStream(videoID string, clientCtx map[string]interface{}, userAgent, visitorData string) (string, error) {
	body := map[string]interface{}{
		"context":        clientCtx,
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to encode YouTube player request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://youtubei.googleapis.com/youtubei/v1/player?prettyPrint=false", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create YouTube player request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if visitorData != "" {
		req.Header.Set("X-Goog-Visitor-Id", visitorData)
	}

	resp, err := y.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("YouTube player request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("YouTube player endpoint returned status %d", resp.StatusCode)
	}

	var playerResp ytPlayerResponse
	if err := json.NewDecoder(resp.Body).Decode(&playerResp); err != nil {
		return "", fmt.Errorf("failed to parse YouTube player response: %w", err)
	}

	if playerResp.PlayabilityStatus.Status != "OK" && playerResp.PlayabilityStatus.Status != "" {
		return "", fmt.Errorf("YouTube playability check failed: %s (%s)", playerResp.PlayabilityStatus.Status, playerResp.PlayabilityStatus.Reason)
	}

	streamURL := y.extractBestAudioURL(&playerResp)
	if streamURL == "" {
		return "", fmt.Errorf("no audio format with direct URL")
	}

	return streamURL, nil
}

func (y *YouTubeProvider) DownloadStreamTo(streamURL string, destPath string) error {
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	var curr int64 = 0
	chunkSize := int64(512 * 1024)

	for {
		req, err := http.NewRequest(http.MethodGet, streamURL, nil)
		if err != nil {
			f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		req.Header.Set("User-Agent", ytVRUserAgent)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", curr, curr+chunkSize-1))

		resp, err := y.client.Do(req)
		if err != nil {
			break
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			break
		}
		n, copyErr := io.Copy(f, resp.Body)
		resp.Body.Close()
		if copyErr != nil || n == 0 {
			break
		}
		curr += n
	}
	f.Close()

	if curr < 10000 {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("downloaded insufficient audio data: %d bytes", curr)
	}

	_ = os.Rename(tmpPath, destPath)
	return nil
}

func (y *YouTubeProvider) Resolve(track *core.Track) (string, error) {
	if track.LocalPath != "" {
		return track.LocalPath, nil
	}

	cacheDir := filepath.Join(os.TempDir(), "rhythm_cache")
	_ = os.MkdirAll(cacheDir, 0755)

	videoID := track.SourceID
	if matches := ytVideoIDRegex.FindStringSubmatch(videoID); len(matches) > 1 {
		videoID = matches[1]
	}
	if videoID == "" {
		videoID = uuid.New().String()
	}

	cachedFile := filepath.Join(cacheDir, fmt.Sprintf("yt_%s.m4a", videoID))
	if fi, err := os.Stat(cachedFile); err == nil && fi.Size() > 50000 {
		track.LocalPath = cachedFile
		track.StreamURL = ""
		return cachedFile, nil
	}

	rawURL, err := y.ResolveRaw(track)
	if err != nil {
		return "", err
	}

	err = y.DownloadStreamTo(rawURL, cachedFile)
	if err != nil {
		return "", fmt.Errorf("failed to stream and cache YouTube audio: %w", err)
	}

	track.LocalPath = cachedFile
	track.StreamURL = ""
	return cachedFile, nil
}

func extractText(obj interface{}) string {
	if obj == nil {
		return ""
	}
	m, ok := obj.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", obj)
	}

	if simple, ok := m["simpleText"].(string); ok && simple != "" {
		return simple
	}

	if runs, ok := m["runs"].([]interface{}); ok && len(runs) > 0 {
		var b strings.Builder
		for _, r := range runs {
			if rMap, ok := r.(map[string]interface{}); ok {
				if t, ok := rMap["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}

	return ""
}

func parseDurationToSeconds(durStr string) float64 {
	if durStr == "" {
		return 0
	}
	parts := strings.Split(durStr, ":")
	var total float64
	for _, p := range parts {
		val, _ := strconv.ParseFloat(strings.TrimSpace(p), 64)
		total = total*60 + val
	}
	return total
}
