package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/dhowden/tag"
)

type ArtMode int

const (
	ArtModeHalfBlock ArtMode = iota
	ArtModeASCIIView
	ArtModeCyberpunkASCII
	ArtModeBraille
)

var (
	httpClient = &http.Client{
		Timeout: 5 * time.Second,
	}

	artCache   = make(map[string][]string)
	artCacheMu sync.RWMutex

	bayer8x8 = [8][8]float64{
		{0, 32, 8, 40, 2, 34, 10, 42},
		{48, 16, 56, 24, 50, 18, 58, 26},
		{12, 44, 4, 36, 14, 46, 6, 38},
		{60, 28, 52, 20, 62, 30, 54, 22},
		{3, 35, 11, 43, 1, 33, 9, 41},
		{51, 19, 59, 27, 49, 17, 57, 25},
		{15, 47, 7, 39, 13, 45, 5, 37},
		{63, 31, 55, 23, 61, 29, 53, 21},
	}
)

func GenerateArtwork(track *core.Track, targetWidth, targetHeight int, mode ArtMode) ([]string, error) {
	if track == nil {
		return nil, fmt.Errorf("nil track")
	}

	cacheKey := track.ID
	if cacheKey == "" {
		cacheKey = track.SourceID
	}
	if cacheKey == "" {
		cacheKey = track.LocalPath
	}
	fullKey := fmt.Sprintf("%s_%dx%d_m%d_v5", cacheKey, targetWidth, targetHeight, mode)

	artCacheMu.RLock()
	if cached, ok := artCache[fullKey]; ok {
		artCacheMu.RUnlock()
		return cached, nil
	}
	artCacheMu.RUnlock()

	img, err := loadTrackImage(track)
	if err != nil || img == nil {
		return nil, err
	}

	var lines []string
	switch mode {
	case ArtModeHalfBlock:
		lines = ImageToHalfBlock(img, targetWidth, targetHeight)
	case ArtModeCyberpunkASCII:
		lines = ImageToCyberpunkASCII(img, targetWidth, targetHeight)
	case ArtModeBraille:
		lines = ImageToBraille(img, targetWidth, targetHeight)
	case ArtModeASCIIView:
		lines = ImageToASCIIView(img, targetWidth, targetHeight)
	default:
		lines = ImageToHalfBlock(img, targetWidth, targetHeight)
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("failed to generate artwork lines")
	}

	artCacheMu.Lock()
	artCache[fullKey] = lines
	artCacheMu.Unlock()

	return lines, nil
}

func visibleWidth(s string) int {
	inEscape := false
	w := 0
	for _, r := range s {
		if r == 0x1b {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		w++
	}
	return w
}

func GenerateFallbackArtwork(targetWidth, targetHeight int, title, artist string) []string {
	if targetWidth <= 0 || targetHeight <= 0 {
		return nil
	}

	vinylLines := []string{
		"\x1b[38;2;86;95;137m   .──────────────.   \x1b[0m",
		"\x1b[38;2;86;95;137m  /  \x1b[38;2;122;162;247m.──────────.\x1b[38;2;86;95;137m  \\  \x1b[0m",
		"\x1b[38;2;86;95;137m /  \x1b[38;2;122;162;247m/   \x1b[38;2;125;207;255m.────.\x1b[38;2;122;162;247m   \\  \x1b[38;2;86;95;137m\\ \x1b[0m",
		"\x1b[38;2;86;95;137m│  \x1b[38;2;122;162;247m│   \x1b[38;2;125;207;255m/  ●   \\\x1b[38;2;122;162;247m   │  \x1b[38;2;86;95;137m│\x1b[0m",
		"\x1b[38;2;86;95;137m│  \x1b[38;2;122;162;247m│  \x1b[38;2;125;207;255m│  \x1b[1;38;2;187;154;247m(✦)\x1b[0m\x1b[38;2;125;207;255m   │\x1b[38;2;122;162;247m  │  \x1b[38;2;86;95;137m│\x1b[0m",
		"\x1b[38;2;86;95;137m│  \x1b[38;2;122;162;247m│   \x1b[38;2;125;207;255m\\  ●   /\x1b[38;2;122;162;247m   │  \x1b[38;2;86;95;137m│\x1b[0m",
		"\x1b[38;2;86;95;137m \\  \x1b[38;2;122;162;247m\\   \x1b[38;2;125;207;255m`────'\x1b[38;2;122;162;247m   /  \x1b[38;2;86;95;137m/ \x1b[0m",
		"\x1b[38;2;86;95;137m  \\  \x1b[38;2;122;162;247m`──────────'\x1b[38;2;86;95;137m  /  \x1b[0m",
		"\x1b[38;2;86;95;137m   `──────────────'   \x1b[0m",
	}

	padTop := (targetHeight - len(vinylLines)) / 2
	if padTop < 0 {
		padTop = 0
	}

	var res []string
	emptyLine := strings.Repeat(" ", targetWidth)
	for i := 0; i < padTop; i++ {
		res = append(res, emptyLine)
	}

	for _, vl := range vinylLines {
		if len(res) >= targetHeight {
			break
		}
		w := visibleWidth(vl)
		padLeft := (targetWidth - w) / 2
		if padLeft < 0 {
			padLeft = 0
		}
		padRight := targetWidth - w - padLeft
		if padRight < 0 {
			padRight = 0
		}
		res = append(res, strings.Repeat(" ", padLeft)+vl+strings.Repeat(" ", padRight))
	}

	for len(res) < targetHeight {
		res = append(res, emptyLine)
	}
	return res
}

func GetTrackCoverThumbnail(track *core.Track, targetWidth, targetHeight int) []string {
	return GetTrackCoverThumbnailProto(track, targetWidth, targetHeight, ProtocolSixel)
}

func GetCachedThumbnail(track *core.Track, targetWidth, targetHeight int) []string {
	return GetCachedThumbnailProto(track, targetWidth, targetHeight, ProtocolSixel)
}

func GenerateArtworkASCII(track *core.Track, targetWidth, targetHeight int) ([]string, error) {
	return GenerateArtwork(track, targetWidth, targetHeight, ArtModeASCIIView)
}

func GetCachedArtwork(track *core.Track, targetWidth, targetHeight int, mode ArtMode) []string {
	if track == nil {
		return nil
	}
	cacheKey := track.ID
	if cacheKey == "" {
		cacheKey = track.SourceID
	}
	if cacheKey == "" {
		cacheKey = track.LocalPath
	}
	fullKey := fmt.Sprintf("%s_%dx%d_m%d_v5", cacheKey, targetWidth, targetHeight, mode)

	artCacheMu.RLock()
	defer artCacheMu.RUnlock()
	return artCache[fullKey]
}

func GetCachedArtworkASCII(track *core.Track, targetWidth, targetHeight int) []string {
	return GetCachedArtwork(track, targetWidth, targetHeight, ArtModeBraille)
}

func LoadTrackImage(track *core.Track) (image.Image, error) {
	return loadTrackImage(track)
}

func GetTrackArtworkPathOrURL(track *core.Track) string {
	if track == nil {
		return ""
	}

	artURL := track.ArtworkURL
	if artURL == "" && len(track.SourceID) == 11 {
		artURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", track.SourceID)
	}

	if track.LocalPath != "" {
		dir := filepath.Dir(track.LocalPath)
		coverNames := []string{"cover.jpg", "cover.png", "folder.jpg", "album.jpg", "front.jpg"}
		for _, name := range coverNames {
			coverPath := filepath.Join(dir, name)
			if _, err := os.Stat(coverPath); err == nil {
				return coverPath
			}
		}

		if f, err := os.Open(track.LocalPath); err == nil {
			m, err := tag.ReadFrom(f)
			f.Close()
			if err == nil && m != nil && m.Picture() != nil && len(m.Picture().Data) > 0 {
				cacheDir := filepath.Join(os.TempDir(), "rhythm_cache")
				_ = os.MkdirAll(cacheDir, 0755)
				cacheFile := filepath.Join(cacheDir, fmt.Sprintf("cover_%s.%s", track.ID, m.Picture().Ext))
				if _, err := os.Stat(cacheFile); err != nil {
					_ = os.WriteFile(cacheFile, m.Picture().Data, 0644)
				}
				return cacheFile
			}
		}
	}

	return artURL
}

func fetchImageURL(u string) (image.Image, error) {
	if u == "" {
		return nil, fmt.Errorf("empty url")
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("read body failed: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

type iTunesSearchResult struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		ArtworkUrl100 string `json:"artworkUrl100"`
	} `json:"results"`
}

func cleanMusicQuery(artist, title string) string {
	q := title
	if artist != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(artist)) && !strings.EqualFold(artist, "Coke Studio India") {
		q = artist + " " + title
	}
	re := []string{
		"(official video)", "(official music video)", "[official video]", "[official music video]",
		"(lyrics)", "[lyrics]", "(lyric video)", "[lyric video]",
		"(audio)", "[audio]", "(visualizer)", "[visualizer]",
		"official video", "official audio", "coke studio india", "coke studio",
	}
	lower := strings.ToLower(q)
	for _, r := range re {
		lower = strings.ReplaceAll(lower, r, "")
	}
	lower = strings.ReplaceAll(lower, "|", " ")
	lower = strings.ReplaceAll(lower, "-", " ")
	lower = strings.Join(strings.Fields(lower), " ")
	return lower
}

func fetchOfficialAlbumArt(artist, title string) string {
	q := cleanMusicQuery(artist, title)
	if q == "" {
		return ""
	}
	endpoint := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=song&limit=1", url.QueryEscape(q))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var res iTunesSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || len(res.Results) == 0 {
		return ""
	}
	art := res.Results[0].ArtworkUrl100
	if art != "" {
		art = strings.ReplaceAll(art, "100x100bb", "600x600bb")
		return art
	}
	return ""
}

func cropToCenterSquare(img image.Image) image.Image {
	if img == nil {
		return nil
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return img
	}

	minY := bounds.Min.Y
	maxY := bounds.Max.Y

	if w == 480 && h == 360 {
		minY += 45
		maxY -= 45
		h = maxY - minY
	} else {
		for y := bounds.Min.Y; y < bounds.Min.Y+h/4; y++ {
			if !isRowBlack(img, bounds.Min.X, bounds.Max.X, y) {
				minY = y
				break
			}
		}
		for y := bounds.Max.Y - 1; y > bounds.Max.Y-h/4; y-- {
			if !isRowBlack(img, bounds.Min.X, bounds.Max.X, y) {
				maxY = y + 1
				break
			}
		}
		if maxY > minY+10 {
			h = maxY - minY
		}
	}

	side := h
	if w < side {
		side = w
	}
	startX := bounds.Min.X + (w-side)/2
	startY := minY + (h-side)/2

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(startX, startY, startX+side, startY+side))
	}
	return img
}

func isRowBlack(img image.Image, minX, maxX, y int) bool {
	step := (maxX - minX) / 10
	if step < 1 {
		step = 1
	}
	for x := minX; x < maxX; x += step {
		r, g, b, _ := img.At(x, y).RGBA()
		if (r>>8) > 40 || (g>>8) > 40 || (b>>8) > 40 {
			return false
		}
	}
	return true
}

func loadTrackImage(track *core.Track) (image.Image, error) {
	if track.LocalPath != "" {
		if f, err := os.Open(track.LocalPath); err == nil {
			m, err := tag.ReadFrom(f)
			f.Close()
			if err == nil && m != nil && m.Picture() != nil && len(m.Picture().Data) > 0 {
				if img, _, err := image.Decode(bytes.NewReader(m.Picture().Data)); err == nil {
					return cropToCenterSquare(img), nil
				}
			}
		}

		dir := filepath.Dir(track.LocalPath)
		coverNames := []string{"cover.jpg", "cover.png", "folder.jpg", "album.jpg", "front.jpg"}
		for _, name := range coverNames {
			coverPath := filepath.Join(dir, name)
			if f, err := os.Open(coverPath); err == nil {
				img, _, err := image.Decode(f)
				f.Close()
				if err == nil && img != nil {
					return cropToCenterSquare(img), nil
				}
			}
		}
	}

	if track.Title != "" {
		if officialURL := fetchOfficialAlbumArt(track.Artist, track.Title); officialURL != "" {
			if img, err := fetchImageURL(officialURL); err == nil && img != nil {
				return cropToCenterSquare(img), nil
			}
		}
	}

	artURL := track.ArtworkURL
	if artURL == "" && len(track.SourceID) == 11 {
		artURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", track.SourceID)
	}

	if artURL != "" {
		if img, err := fetchImageURL(artURL); err == nil && img != nil {
			return cropToCenterSquare(img), nil
		}
		if strings.Contains(artURL, "maxresdefault.jpg") && len(track.SourceID) == 11 {
			if img, err := fetchImageURL(fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", track.SourceID)); err == nil && img != nil {
				return cropToCenterSquare(img), nil
			}
		}
	}

	return nil, fmt.Errorf("no artwork found for track: %s", track.Title)
}

func ImageToBraille(img image.Image, targetWidth, targetHeight int) []string {
	if img == nil || targetWidth <= 0 || targetHeight <= 0 {
		return nil
	}

	bounds := img.Bounds()
	totalCols := targetWidth * 2
	totalRows := targetHeight * 4

	dots := make([][]bool, totalRows)
	colorsR := make([][]uint8, totalRows)
	colorsG := make([][]uint8, totalRows)
	colorsB := make([][]uint8, totalRows)
	lumGrid := make([][]float64, totalRows)

	minLum := 255.0
	maxLum := 0.0

	for y := 0; y < totalRows; y++ {
		dots[y] = make([]bool, totalCols)
		colorsR[y] = make([]uint8, totalCols)
		colorsG[y] = make([]uint8, totalCols)
		colorsB[y] = make([]uint8, totalCols)
		lumGrid[y] = make([]float64, totalCols)

		for x := 0; x < totalCols; x++ {
			r, g, b, a := sampleAverageColor(img, bounds, x, y, totalCols, totalRows)
			colorsR[y][x] = r
			colorsG[y][x] = g
			colorsB[y][x] = b

			if a < 25 {
				lumGrid[y][x] = 0
				continue
			}

			lum := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
			lumGrid[y][x] = lum
			if lum < minLum {
				minLum = lum
			}
			if lum > maxLum {
				maxLum = lum
			}
		}
	}

	diffLum := maxLum - minLum
	if diffLum < 15 {
		diffLum = 15
	}

	for y := 0; y < totalRows; y++ {
		for x := 0; x < totalCols; x++ {
			norm := (lumGrid[y][x] - minLum) / diffLum
			if norm < 0 {
				norm = 0
			}
			if norm > 1 {
				norm = 1
			}

			curved := math.Pow(norm, 1.15)

			bayerThreshold := (bayer8x8[y%8][x%8] + 0.5) / 64.0
			if curved > bayerThreshold {
				dots[y][x] = true
			}
		}
	}

	dotOffsets := [4][2]uint{
		{0, 3},
		{1, 4},
		{2, 5},
		{6, 7},
	}

	var lines []string
	for cy := 0; cy < targetHeight; cy++ {
		var line strings.Builder
		for cx := 0; cx < targetWidth; cx++ {
			var mask uint
			var sumR, sumG, sumB uint32
			var activeCount uint32

			for dy := 0; dy < 4; dy++ {
				py := cy*4 + dy
				for dx := 0; dx < 2; dx++ {
					px := cx*2 + dx
					if py < totalRows && px < totalCols && dots[py][px] {
						mask |= (1 << dotOffsets[dy][dx])
						sumR += uint32(colorsR[py][px])
						sumG += uint32(colorsG[py][px])
						sumB += uint32(colorsB[py][px])
						activeCount++
					}
				}
			}

			if mask == 0 || activeCount == 0 {
				line.WriteRune(' ')
			} else {
				avgR := boostColorVal(uint8(sumR/activeCount), 1.3)
				avgG := boostColorVal(uint8(sumG/activeCount), 1.3)
				avgB := boostColorVal(uint8(sumB/activeCount), 1.3)
				r := rune(0x2800 + mask)
				line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm%c\x1b[0m", avgR, avgG, avgB, r))
			}
		}
		lines = append(lines, line.String())
	}

	return lines
}

func ImageToCyberpunkASCII(img image.Image, targetWidth, targetHeight int) []string {
	if img == nil || targetWidth <= 0 || targetHeight <= 0 {
		return nil
	}

	bounds := img.Bounds()
	bW := float64(bounds.Dx())
	bH := float64(bounds.Dy())
	if bW <= 0 || bH <= 0 {
		return nil
	}

	imgAR := bW / bH
	charBoxAR := imgAR * 2.0

	fitW := targetWidth
	fitH := targetHeight

	if float64(targetWidth)/float64(targetHeight) > charBoxAR {
		fitH = targetHeight
		fitW = int(float64(targetHeight)*charBoxAR + 0.5)
	} else {
		fitW = targetWidth
		fitH = int(float64(targetWidth)/charBoxAR + 0.5)
	}

	if fitW > targetWidth {
		fitW = targetWidth
	}
	if fitH > targetHeight {
		fitH = targetHeight
	}
	if fitW < 4 {
		fitW = 4
	}
	if fitH < 4 {
		fitH = 4
	}

	padLeft := (targetWidth - fitW) / 2
	padTop := (targetHeight - fitH) / 2

	gridR := make([][]float64, fitH)
	gridG := make([][]float64, fitH)
	gridB := make([][]float64, fitH)
	gridLum := make([][]float64, fitH)

	minLum := 255.0
	maxLum := 0.0

	for y := 0; y < fitH; y++ {
		gridR[y] = make([]float64, fitW)
		gridG[y] = make([]float64, fitW)
		gridB[y] = make([]float64, fitW)
		gridLum[y] = make([]float64, fitW)

		for x := 0; x < fitW; x++ {
			r, g, b, a := sampleAverageColor(img, bounds, x, y, fitW, fitH)
			if a < 25 {
				gridLum[y][x] = 0
				continue
			}

			lum := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
			gridR[y][x] = float64(r)
			gridG[y][x] = float64(g)
			gridB[y][x] = float64(b)
			gridLum[y][x] = lum

			if lum < minLum {
				minLum = lum
			}
			if lum > maxLum {
				maxLum = lum
			}
		}
	}

	diffLum := maxLum - minLum
	if diffLum < 20 {
		diffLum = 20
	}

	edgeMag := make([][]float64, fitH)
	edgeAngle := make([][]float64, fitH)

	for y := 0; y < fitH; y++ {
		edgeMag[y] = make([]float64, fitW)
		edgeAngle[y] = make([]float64, fitW)
		for x := 0; x < fitW; x++ {
			if y == 0 || y == fitH-1 || x == 0 || x == fitW-1 {
				continue
			}
			gx := (gridLum[y-1][x+1] + 2*gridLum[y][x+1] + gridLum[y+1][x+1]) -
				(gridLum[y-1][x-1] + 2*gridLum[y][x-1] + gridLum[y+1][x-1])
			gy := (gridLum[y+1][x-1] + 2*gridLum[y+1][x] + gridLum[y+1][x+1]) -
				(gridLum[y-1][x-1] + 2*gridLum[y-1][x] + gridLum[y-1][x+1])

			mag := math.Hypot(gx, gy)
			edgeMag[y][x] = mag
			edgeAngle[y][x] = math.Atan2(gy, gx) * (180.0 / math.Pi)
		}
	}

	ramp := []rune(" .'`,:;!~+i1tfcoadhkXW8#%@█")
	numLevels := float64(len(ramp) - 1)

	var lines []string
	leftPadStr := strings.Repeat(" ", padLeft)
	rightPadStr := strings.Repeat(" ", targetWidth-fitW-padLeft)
	emptyLine := strings.Repeat(" ", targetWidth)

	for y := 0; y < padTop; y++ {
		lines = append(lines, emptyLine)
	}

	for y := 0; y < fitH; y++ {
		var line strings.Builder
		line.WriteString(leftPadStr)

		for x := 0; x < fitW; x++ {
			r := gridR[y][x]
			g := gridG[y][x]
			b := gridB[y][x]
			lum := gridLum[y][x]

			rB := boostColorVal(uint8(r), 1.25)
			gB := boostColorVal(uint8(g), 1.25)
			bB := boostColorVal(uint8(b), 1.25)

			mag := edgeMag[y][x]
			ang := edgeAngle[y][x]

			if mag > 260.0 {
				var edgeChar rune
				absAng := math.Abs(ang)
				if absAng < 22.5 || absAng > 157.5 {
					edgeChar = '─'
				} else if absAng >= 67.5 && absAng <= 112.5 {
					edgeChar = '│'
				} else if ang > 0 {
					edgeChar = '/'
				} else {
					edgeChar = '\\'
				}

				line.WriteString(fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm%c\x1b[0m", rB, gB, bB, edgeChar))
				continue
			}

			norm := (lum - minLum) / diffLum
			if norm < 0 {
				norm = 0
			}
			if norm > 1 {
				norm = 1
			}

			curved := math.Pow(norm, 1.1)

			bayerOffset := (bayer8x8[y%8][x%8] - 31.5) / 64.0 * 0.18
			ditheredVal := curved + bayerOffset
			if ditheredVal < 0 {
				ditheredVal = 0
			}
			if ditheredVal > 1 {
				ditheredVal = 1
			}

			idx := int(ditheredVal*numLevels + 0.5)
			if idx < 0 {
				idx = 0
			}
			if idx >= len(ramp) {
				idx = len(ramp) - 1
			}

			ch := ramp[idx]

			maxC := math.Max(r, math.Max(g, b))
			minC := math.Min(r, math.Min(g, b))
			saturation := 0.0
			if maxC > 0 {
				saturation = (maxC - minC) / maxC
			}

			if ch == ' ' {
				if saturation > 0.35 && maxC > 30 {

					line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm.\x1b[0m", rB, gB, bB))
				} else {
					line.WriteRune(' ')
				}
				continue
			}

			if lum > 195 {
				line.WriteString(fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm%c\x1b[0m", rB, gB, bB, ch))
			} else {
				line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm%c\x1b[0m", rB, gB, bB, ch))
			}
		}

		line.WriteString(rightPadStr)
		lines = append(lines, line.String())
	}

	for len(lines) < targetHeight {
		lines = append(lines, emptyLine)
	}

	return lines
}

func boostColorVal(val uint8, factor float64) int {
	v := float64(val) * factor
	if v > 255 {
		return 255
	}
	return int(v)
}

func ImageToHalfBlock(img image.Image, targetWidth, targetHeight int) []string {
	if img == nil || targetWidth <= 0 || targetHeight <= 0 {
		return nil
	}

	bounds := img.Bounds()
	bWidth := bounds.Dx()
	bHeight := bounds.Dy()
	if bWidth <= 0 || bHeight <= 0 {
		return nil
	}

	imgAR := float64(bWidth) / float64(bHeight)
	charBoxAR := imgAR * 2.0

	fitW := targetWidth
	fitH := targetHeight

	if float64(targetWidth)/float64(targetHeight) > charBoxAR {
		fitH = targetHeight
		fitW = int(float64(targetHeight)*charBoxAR + 0.5)
	} else {
		fitW = targetWidth
		fitH = int(float64(targetWidth)/charBoxAR + 0.5)
	}

	if fitW > targetWidth {
		fitW = targetWidth
	}
	if fitH > targetHeight {
		fitH = targetHeight
	}
	if fitW < 4 {
		fitW = 4
	}
	if fitH < 4 {
		fitH = 4
	}

	padLeft := (targetWidth - fitW) / 2
	padTop := (targetHeight - fitH) / 2

	fitPixelRows := fitH * 2
	var lines []string

	leftPadStr := strings.Repeat(" ", padLeft)
	rightPadStr := strings.Repeat(" ", targetWidth-fitW-padLeft)
	emptyLine := strings.Repeat(" ", targetWidth)

	for y := 0; y < padTop; y++ {
		lines = append(lines, emptyLine)
	}

	for cy := 0; cy < fitH; cy++ {
		var line strings.Builder
		line.WriteString(leftPadStr)

		yTop := cy * 2
		yBot := cy*2 + 1

		for cx := 0; cx < fitW; cx++ {
			rt, gt, bt, at := sampleAverageColor(img, bounds, cx, yTop, fitW, fitPixelRows)
			rb, gb, bb, ab := sampleAverageColor(img, bounds, cx, yBot, fitW, fitPixelRows)

			if at < 35 && ab < 35 {
				line.WriteRune(' ')
			} else if at >= 35 && ab < 35 {
				line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm▀\x1b[0m", rt, gt, bt))
			} else if at < 35 && ab >= 35 {
				line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm▄\x1b[0m", rb, gb, bb))
			} else {
				line.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀\x1b[0m", rt, gt, bt, rb, gb, bb))
			}
		}

		line.WriteString(rightPadStr)
		lines = append(lines, line.String())
	}

	for len(lines) < targetHeight {
		lines = append(lines, emptyLine)
	}

	return lines
}

func sampleAverageColor(img image.Image, bounds image.Rectangle, cx, cy, totalX, totalY int) (uint8, uint8, uint8, uint8) {
	bWidth := bounds.Dx()
	bHeight := bounds.Dy()

	startX := bounds.Min.X + (cx*bWidth)/totalX
	endX := bounds.Min.X + ((cx+1)*bWidth)/totalX
	if endX <= startX {
		endX = startX + 1
	}

	startY := bounds.Min.Y + (cy*bHeight)/totalY
	endY := bounds.Min.Y + ((cy+1)*bHeight)/totalY
	if endY <= startY {
		endY = startY + 1
	}

	var totalR, totalG, totalB, totalA uint32
	var count uint32

	stepX := 1
	if (endX - startX) > 4 {
		stepX = (endX - startX) / 4
	}
	stepY := 1
	if (endY - startY) > 4 {
		stepY = (endY - startY) / 4
	}

	for y := startY; y < endY; y += stepY {
		for x := startX; x < endX; x += stepX {
			c := img.At(x, y)
			r, g, b, a := c.RGBA()
			totalR += r >> 8
			totalG += g >> 8
			totalB += b >> 8
			totalA += a >> 8
			count++
		}
	}

	if count == 0 {
		return 0, 0, 0, 0
	}

	return uint8(totalR / count), uint8(totalG / count), uint8(totalB / count), uint8(totalA / count)
}

func ImageToASCII(img image.Image, targetWidth, targetHeight int) []string {
	return ImageToBraille(img, targetWidth, targetHeight)
}

var _ color.Color = color.RGBA{}
