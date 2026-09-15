package lyrics

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
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

	if lrc, err := fetchLRCLIB(track); err == nil && lrc != nil {
		if lrc.Synced {
			lrc.IsFallback = false
			cacheResult(lrc)
			return lrc, nil
		}
	}

	if lrc, err := fetchLetras(track); err == nil && lrc != nil {
		lrc.IsFallback = false
		cacheResult(lrc)
		return lrc, nil
	}

	if lrc, err := fetchLRCLIB(track); err == nil && lrc != nil {
		lrc.IsFallback = false
		cacheResult(lrc)
		return lrc, nil
	}

	artist, title := resolveArtistAndTitle(track)
	placeholder := &Lyrics{
		TrackID:    track.ID,
		Title:      track.Title,
		Artist:     track.Artist,
		Synced:     false,
		IsFallback: true,
		Lines: []LyricLine{
			{Time: 0 * time.Second, Text: "♪ Lyrics not found ♪"},
			{Time: 4 * time.Second, Text: fmt.Sprintf("Title: %s", title)},
			{Time: 8 * time.Second, Text: fmt.Sprintf("Artist: %s", artist)},
			{Time: 12 * time.Second, Text: "Place a .lrc file beside the song for offline sync"},
		},
	}

	return placeholder, nil
}

type lrclibSearchItem struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
}

var cleanPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*\([^)]*(?:official|lyrics?|video|audio|mv|visualizer|color\s*coded|hd|4k|prod\.|remastered)[^)]*\)`),
	regexp.MustCompile(`(?i)\s*\[[^\]]*(?:official|lyrics?|video|audio|mv|visualizer|color\s*coded|hd|4k|prod\.|remastered)[^\]]*\]`),
	regexp.MustCompile(`(?i)\s*-\s*Topic$`),
	regexp.MustCompile(`(?i)VEVO$`),
}

var featPattern = regexp.MustCompile(`(?i)\s*[([]\s*(?:ft\.?|feat\.?|featuring)\s+[^)\]]+[)\]]`)

func cleanMetadata(text string, removeFeaturing bool) string {
	res := text
	for _, p := range cleanPatterns {
		res = p.ReplaceAllString(res, "")
	}
	if removeFeaturing {
		res = featPattern.ReplaceAllString(res, "")
	}
	return strings.TrimSpace(res)
}

func parseTrackQuery(query string) (string, string) {
	cleaned := cleanMetadata(query, true)
	separators := []string{" - ", " – ", " — ", " ~ "}
	for _, sep := range separators {
		idx := strings.Index(cleaned, sep)
		if idx > 0 && idx < len(cleaned)-len(sep) {
			artist := strings.TrimSpace(cleaned[:idx])
			title := strings.TrimSpace(cleaned[idx+len(sep):])
			if len(artist) > 0 && len(title) > 0 {
				return artist, title
			}
		}
	}
	return "", cleaned
}

func normalizeComparableText(text string) string {
	t := cleanMetadata(text, true)
	t = strings.ToLower(t)
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func resolveArtistAndTitle(track *core.Track) (string, string) {
	if track == nil {
		return "", ""
	}

	parsedArtist, parsedTitle := parseTrackQuery(track.Title)

	artist := track.Artist
	title := parsedTitle
	if title == "" {
		title = track.Title
	}

	if artist == "" || strings.EqualFold(artist, "Unknown") || strings.EqualFold(artist, "YouTube") || strings.EqualFold(artist, "Various Artists") {
		if parsedArtist != "" {
			artist = parsedArtist
		}
	} else if parsedArtist != "" && !strings.Contains(strings.ToLower(track.Title), strings.ToLower(track.Artist)) {
		artist = track.Artist
	}

	title = cleanMetadata(title, true)
	artist = cleanMetadata(artist, true)

	return artist, title
}

func selectBestLRCLIBMatch(results []lrclibSearchItem, targetTitle, targetArtist string, targetDuration float64) *lrclibSearchItem {
	normTitle := normalizeComparableText(targetTitle)
	normArtist := normalizeComparableText(targetArtist)

	if normTitle == "" {
		return nil
	}

	type scoredItem struct {
		item  *lrclibSearchItem
		score int
	}

	var scored []scoredItem

	for i := range results {
		it := &results[i]
		if it.Instrumental {
			continue
		}
		if it.SyncedLyrics == "" && it.PlainLyrics == "" {
			continue
		}

		itTitle := normalizeComparableText(it.TrackName)
		itArtist := normalizeComparableText(it.ArtistName)

		titleExact := (itTitle == normTitle)
		titleContains := strings.Contains(itTitle, normTitle) || strings.Contains(normTitle, itTitle)
		artistExact := (normArtist != "" && itArtist == normArtist)
		artistContains := (normArtist != "" && (strings.Contains(itArtist, normArtist) || strings.Contains(normArtist, itArtist)))

		if !titleExact && !titleContains {
			continue
		}

		durDelta := 0.0
		if targetDuration > 0 && it.Duration > 0 {
			durDelta = math.Abs(targetDuration - it.Duration)
			if durDelta > 20.0 {
				continue
			}
		}

		score := 0
		if it.SyncedLyrics != "" {
			score += 100
		}
		if titleExact {
			score += 50
		} else if titleContains {
			score += 20
		}
		if artistExact {
			score += 40
		} else if artistContains {
			score += 15
		}
		if targetDuration > 0 && it.Duration > 0 {
			if durDelta <= 3.0 {
				score += 30
			} else if durDelta <= 8.0 {
				score += 15
			} else if durDelta <= 15.0 {
				score += 5
			}
		}

		scored = append(scored, scoredItem{item: it, score: score})
	}

	if len(scored) == 0 {
		return nil
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	return scored[0].item
}

func fetchLRCLIB(track *core.Track) (*Lyrics, error) {
	artist, title := resolveArtistAndTitle(track)
	if title == "" {
		return nil, fmt.Errorf("empty title")
	}

	endpoint := "https://lrclib.net/api/get"
	params := url.Values{}
	params.Set("track_name", title)
	if artist != "" {
		params.Set("artist_name", artist)
	}
	if track.Album != "" {
		params.Set("album_name", track.Album)
	}
	if track.Duration > 0 {
		params.Set("duration", fmt.Sprintf("%d", int(track.Duration)))
	}

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	if req, err := http.NewRequest("GET", reqURL, nil); err == nil {
		req.Header.Set("User-Agent", "RhythmMusicPlayer/2.0")
		if resp, err := httpClient.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var lresp lrclibSearchItem
				if err := json.NewDecoder(resp.Body).Decode(&lresp); err == nil && !lresp.Instrumental {
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
		artist + " " + title,
		title,
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
		sReq.Header.Set("User-Agent", "RhythmMusicPlayer/2.0")
		sResp, err := httpClient.Do(sReq)
		if err != nil {
			continue
		}
		defer sResp.Body.Close()

		if sResp.StatusCode == http.StatusOK {
			var items []lrclibSearchItem
			if err := json.NewDecoder(sResp.Body).Decode(&items); err == nil && len(items) > 0 {
				best := selectBestLRCLIBMatch(items, title, artist, track.Duration)
				if best != nil {
					if best.SyncedLyrics != "" {
						return ParseLRC(best.SyncedLyrics), nil
					}
					if best.PlainLyrics != "" {
						return ParseLRC(best.PlainLyrics), nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("lyrics not found on lrclib")
}

func parseLetrasSubtitle(subRaw string) []LyricLine {
	var entries [][]string
	if err := json.Unmarshal([]byte(subRaw), &entries); err != nil {
		return nil
	}
	var res []LyricLine
	for _, entry := range entries {
		if len(entry) < 2 {
			continue
		}
		txt := strings.TrimSpace(entry[0])
		if txt == "" {
			continue
		}
		sec, err := strconv.ParseFloat(entry[1], 64)
		if err != nil {
			continue
		}
		res = append(res, LyricLine{
			Time: time.Duration(sec * float64(time.Second)),
			Text: txt,
		})
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Time < res[j].Time
	})
	return res
}

func fetchLetras(track *core.Track) (*Lyrics, error) {
	artist, title := resolveArtistAndTitle(track)
	if title == "" {
		return nil, fmt.Errorf("empty title")
	}

	q := strings.TrimSpace(artist + " " + title)
	solrURL := fmt.Sprintf("https://solr.sscdn.co/letras/m1/?q=%s&wt=json&callback=LetrasSug", url.QueryEscape(q))
	req, err := http.NewRequest("GET", solrURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	raw := string(body)
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start == -1 || end <= start {
		return nil, fmt.Errorf("invalid jsonp")
	}
	jsonStr := raw[start+1 : end]

	var solrRes struct {
		Response struct {
			Docs []struct {
				Txt string `json:"txt"`
				Art string `json:"art"`
				Dns string `json:"dns"`
				Url string `json:"url"`
				T   string `json:"t"`
			} `json:"docs"`
		} `json:"response"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &solrRes); err != nil {
		return nil, err
	}

	normTitle := normalizeComparableText(title)
	normArtist := normalizeComparableText(artist)

	var bestDns, bestUrl string
	for _, doc := range solrRes.Response.Docs {
		if doc.T != "2" || doc.Dns == "" || doc.Url == "" {
			continue
		}
		docTitle := normalizeComparableText(doc.Txt)
		docArt := normalizeComparableText(doc.Art)
		if docTitle == normTitle && (normArtist == "" || docArt == normArtist) {
			bestDns = doc.Dns
			bestUrl = doc.Url
			break
		}
		if docTitle == normTitle {
			bestDns = doc.Dns
			bestUrl = doc.Url
		}
	}

	if bestDns == "" && len(solrRes.Response.Docs) > 0 {
		for _, doc := range solrRes.Response.Docs {
			if doc.T == "2" && doc.Dns != "" && doc.Url != "" {
				docTitle := normalizeComparableText(doc.Txt)
				if strings.Contains(docTitle, normTitle) || strings.Contains(normTitle, docTitle) {
					bestDns = doc.Dns
					bestUrl = doc.Url
					break
				}
			}
		}
	}

	if bestDns == "" || bestUrl == "" {
		return nil, fmt.Errorf("no matching letras doc")
	}

	pageURL := fmt.Sprintf("https://www.letras.mus.br/%s/%s/", bestDns, bestUrl)
	pReq, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, err
	}
	pReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	pResp, err := httpClient.Do(pReq)
	if err != nil {
		return nil, err
	}
	defer pResp.Body.Close()

	pageBody, err := io.ReadAll(pResp.Body)
	if err != nil {
		return nil, err
	}
	html := string(pageBody)

	omqRe := regexp.MustCompile(`_omq\.push\(\['ui/lyric',\s*({[\s\S]*?})\s*,`)
	match := omqRe.FindStringSubmatch(html)
	if len(match) > 1 {
		var omq struct {
			ID        int    `json:"ID"`
			YoutubeID string `json:"YoutubeID"`
			Name      string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(match[1]), &omq); err == nil && omq.ID != 0 && omq.YoutubeID != "" {
			subURL := fmt.Sprintf("https://www.letras.mus.br/api/v2/subtitle/%d/%s/", omq.ID, omq.YoutubeID)
			sReq, err := http.NewRequest("GET", subURL, nil)
			if err == nil {
				sReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
				if sResp, err := httpClient.Do(sReq); err == nil {
					defer sResp.Body.Close()
					if sResp.StatusCode == http.StatusOK {
						var subApiRes struct {
							Status   string `json:"status"`
							Original struct {
								Subtitle string `json:"Subtitle"`
							} `json:"Original"`
						}
						if err := json.NewDecoder(sResp.Body).Decode(&subApiRes); err == nil {
							if subApiRes.Status != "not found" && subApiRes.Original.Subtitle != "" {
								lines := parseLetrasSubtitle(subApiRes.Original.Subtitle)
								if len(lines) > 0 {
									return &Lyrics{
										Synced: true,
										Lines:  lines,
									}, nil
								}
							}
						}
					}
				}
			}
		}
	}

	lyricDivRe := regexp.MustCompile(`(?i)<div class="lyric-original[^>]*>([\s\S]*?)</div>`)
	divMatch := lyricDivRe.FindStringSubmatch(html)
	if len(divMatch) > 1 {
		text := divMatch[1]
		text = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(text, "\n")
		text = regexp.MustCompile(`(?i)</p>`).ReplaceAllString(text, "\n")
		text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, "")
		parsed := ParseLRC(text)
		if parsed != nil && len(parsed.Lines) > 0 {
			return parsed, nil
		}
	}

	return nil, fmt.Errorf("no lyrics on letras")
}
