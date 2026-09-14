package lyrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
)

type LyricLine struct {
	Time time.Duration `json:"time"`
	Text string        `json:"text"`
}

type Lyrics struct {
	TrackID    string      `json:"track_id"`
	Title      string      `json:"title"`
	Artist     string      `json:"artist"`
	Synced     bool        `json:"synced"`
	Lines      []LyricLine `json:"lines"`
	IsFallback bool        `json:"is_fallback"`
}

var (
	lyricsCache   = make(map[string]*Lyrics)
	lyricsCacheMu sync.RWMutex

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
	}

	lrcTimeRegex = regexp.MustCompile(`\[(\d{1,2}):(\d{2})(?:\.(\d{1,3}))?\]`)
)

func ParseLRC(content string) *Lyrics {
	lines := strings.Split(content, "\n")
	var lyricLines []LyricLine

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		matches := lrcTimeRegex.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {

			continue
		}

		lastMatch := matches[len(matches)-1]
		text := strings.TrimSpace(line[lastMatch[1]:])

		for _, m := range matches {
			minStr := line[m[2]:m[3]]
			secStr := line[m[4]:m[5]]
			msStr := ""
			if m[6] != -1 && m[7] != -1 {
				msStr = line[m[6]:m[7]]
			}

			mins, _ := strconv.Atoi(minStr)
			secs, _ := strconv.Atoi(secStr)
			var millis int
			if len(msStr) == 2 {

				h, _ := strconv.Atoi(msStr)
				millis = h * 10
			} else if len(msStr) == 3 {
				millis, _ = strconv.Atoi(msStr)
			} else if len(msStr) == 1 {
				h, _ := strconv.Atoi(msStr)
				millis = h * 100
			}

			t := time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second + time.Duration(millis)*time.Millisecond
			lyricLines = append(lyricLines, LyricLine{
				Time: t,
				Text: text,
			})
		}
	}

	if len(lyricLines) == 0 {
		var timedLines []LyricLine
		idx := 0
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed != "" && !strings.HasPrefix(trimmed, "[ti:") && !strings.HasPrefix(trimmed, "[ar:") && !strings.HasPrefix(trimmed, "[al:") {
				timedLines = append(timedLines, LyricLine{
					Time: time.Duration(idx) * 4 * time.Second,
					Text: trimmed,
				})
				idx++
			}
		}
		if len(timedLines) > 0 {
			return &Lyrics{
				Synced: true,
				Lines:  timedLines,
			}
		}
		return &Lyrics{
			Synced: false,
			Lines:  nil,
		}
	}

	sort.Slice(lyricLines, func(i, j int) bool {
		return lyricLines[i].Time < lyricLines[j].Time
	})

	return &Lyrics{
		Synced: true,
		Lines:  lyricLines,
	}
}

func GetLyricsCached(track *core.Track) *Lyrics {
	if track == nil {
		return nil
	}

	lyricsCacheMu.RLock()
	defer lyricsCacheMu.RUnlock()

	var res *Lyrics
	if track.ID != "" {
		res = lyricsCache[track.ID]
	}
	if res == nil && track.LocalPath != "" {
		res = lyricsCache[track.LocalPath]
	}
	if res == nil {
		res = lyricsCache[track.Title+"_"+track.Artist]
	}
	if res != nil && res.IsFallback {
		return nil
	}
	return res
}

func FindActiveLineIndex(lyrics *Lyrics, position time.Duration) int {
	if lyrics == nil || len(lyrics.Lines) == 0 {
		return -1
	}

	if !lyrics.Synced {
		return -1
	}

	activeIdx := -1
	for i, l := range lyrics.Lines {
		if l.Time <= position {
			activeIdx = i
		} else {
			break
		}
	}
	return activeIdx
}

func GetLyrics(track *core.Track) (*Lyrics, error) {
	if track == nil {
		return nil, fmt.Errorf("nil track")
	}

	if cached := GetLyricsCached(track); cached != nil {
		return cached, nil
	}

	cacheResult := func(lrc *Lyrics) {
		lrc.TrackID = track.ID
		lrc.Title = track.Title
		lrc.Artist = track.Artist
		lyricsCacheMu.Lock()
		if track.ID != "" {
			lyricsCache[track.ID] = lrc
		}
		if track.LocalPath != "" {
			lyricsCache[track.LocalPath] = lrc
		}
		if track.Title != "" {
			lyricsCache[track.Title+"_"+track.Artist] = lrc
		}
		lyricsCacheMu.Unlock()
	}

	if track.LocalPath != "" {
		ext := filepath.Ext(track.LocalPath)
		lrcPath := strings.TrimSuffix(track.LocalPath, ext) + ".lrc"
		if data, err := os.ReadFile(lrcPath); err == nil {
			parsed := ParseLRC(string(data))
			parsed.IsFallback = false
			cacheResult(parsed)
			return parsed, nil
		}
	}

	if track.Title != "" {
		if lrc, err := fetchLRCLIB(track); err == nil && lrc != nil {
			lrc.IsFallback = false
			cacheResult(lrc)
			return lrc, nil
		}
	}

	cleanTitle := cleanSongTitle(track.Title)
	cleanArtist := cleanArtistName(track.Artist)

	placeholder := &Lyrics{
		TrackID:    track.ID,
		Title:      track.Title,
		Artist:     track.Artist,
		Synced:     false,
		IsFallback: true,
		Lines: []LyricLine{
			{Time: 0 * time.Second, Text: "♪ Lyrics not found on LRCLIB ♪"},
			{Time: 4 * time.Second, Text: fmt.Sprintf("Title: %s", cleanTitle)},
			{Time: 8 * time.Second, Text: fmt.Sprintf("Artist: %s", cleanArtist)},
			{Time: 12 * time.Second, Text: "Place a .lrc file in folder for offline sync"},
		},
	}

	return placeholder, nil
}

type lrclibResponse struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
}

func fetchLRCLIB(track *core.Track) (*Lyrics, error) {
	cleanTitle := cleanSongTitle(track.Title)
	if cleanTitle == "" {
		cleanTitle = track.Title
	}
	cleanArtist := cleanArtistName(track.Artist)

	endpoint := "https://lrclib.net/api/get"
	params := url.Values{}
	params.Set("track_name", cleanTitle)
	if cleanArtist != "" {
		params.Set("artist_name", cleanArtist)
	}
	if track.Album != "" {
		params.Set("album_name", track.Album)
	}
	if track.Duration > 0 {
		params.Set("duration", fmt.Sprintf("%d", int(track.Duration)))
	}

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	if req, err := http.NewRequest("GET", reqURL, nil); err == nil {
		req.Header.Set("User-Agent", "RhythmRhythmMusicPlayer/1.0 (https://github.com/aome510/spotify-player-inspired)")
		if resp, err := httpClient.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var lresp lrclibResponse
				if err := json.NewDecoder(resp.Body).Decode(&lresp); err == nil {
					if lresp.SyncedLyrics != "" {
						return ParseLRC(lresp.SyncedLyrics), nil
					} else if lresp.PlainLyrics != "" {
						return ParseLRC(lresp.PlainLyrics), nil
					}
				}
			}
		}
	}

	queries := []string{
		cleanTitle + " " + cleanArtist,
		cleanTitle,
	}

	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		searchEndpoint := fmt.Sprintf("https://lrclib.net/api/search?q=%s", url.QueryEscape(q))
		sReq, err := http.NewRequest("GET", searchEndpoint, nil)
		if err != nil {
			continue
		}
		sReq.Header.Set("User-Agent", "RhythmRhythmMusicPlayer/1.0")
		sResp, err := httpClient.Do(sReq)
		if err != nil {
			continue
		}
		defer sResp.Body.Close()

		if sResp.StatusCode == http.StatusOK {
			var items []lrclibResponse
			if err := json.NewDecoder(sResp.Body).Decode(&items); err == nil && len(items) > 0 {
				lowerArtist := strings.ToLower(cleanArtist)

				for _, it := range items {
					if it.SyncedLyrics != "" && (lowerArtist == "" || strings.Contains(strings.ToLower(it.ArtistName), lowerArtist)) {
						return ParseLRC(it.SyncedLyrics), nil
					}
				}

				for _, it := range items {
					if it.SyncedLyrics != "" {
						return ParseLRC(it.SyncedLyrics), nil
					}
				}

				if items[0].PlainLyrics != "" {
					return ParseLRC(items[0].PlainLyrics), nil
				}
			}
		}
	}

	return nil, fmt.Errorf("lyrics not found on lrclib")
}

func cleanSongTitle(t string) string {
	re := regexp.MustCompile(`(?i)\(.*?official.*?\)|\[.*?official.*?\]|\(feat\..*?\)|\(with.*?\)|\[.*?remastered.*?\]|\(.*?video.*?\)|\[.*?video.*?\]|\(.*?lyric.*?\)|\[.*?lyric.*?\]|\(audio\)|\[audio\]|\(from .*?\)`)
	clean := re.ReplaceAllString(t, "")
	if idx := strings.Index(clean, " - "); idx != -1 {
		clean = clean[:idx]
	}
	if idx := strings.Index(clean, " | "); idx != -1 {
		clean = clean[:idx]
	}
	clean = strings.TrimSuffix(clean, "...")
	return strings.TrimSpace(clean)
}

func cleanArtistName(a string) string {
	delims := []string{" - ", " – ", " — ", ",", "&", "/", " feat", " ft.", " with "}
	clean := a
	for _, d := range delims {
		if idx := strings.Index(strings.ToLower(clean), d); idx != -1 {
			clean = clean[:idx]
		}
	}
	clean = strings.TrimSuffix(clean, "...")
	return strings.TrimSpace(clean)
}
