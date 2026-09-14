package metadata

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"golang.org/x/term"
)

const (
	ASCIIViewChars        = " .-=+*x#$&X@"
	DefaultCharRatio      = 2.0
	DefaultEdgeThreshold  = 4.0
	EnhancedEdgeThreshold = 1.8
)

type ASCIIViewConfig struct {
	MaxWidth      int
	MaxHeight     int
	EdgeThreshold float64
	CharRatio     float64
	RetroColors   bool
	CenterPadding bool
}

func DefaultConfig() ASCIIViewConfig {
	w, h := DetectTerminalSize()
	return ASCIIViewConfig{
		MaxWidth:      w,
		MaxHeight:     h,
		EdgeThreshold: DefaultEdgeThreshold,
		CharRatio:     DefaultCharRatio,
		RetroColors:   false,
		CenterPadding: false,
	}
}

func DetectTerminalSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil && w > 0 && h > 0 {
		return w, h
	}
	return 64, 48
}

type hsv struct {
	h, s, v float64
}

func rgbToHSV(red, green, blue float64) hsv {
	max := red
	if green > max {
		max = green
	}
	if blue > max {
		max = blue
	}

	min := red
	if green < min {
		min = green
	}
	if blue < min {
		min = blue
	}

	val := max
	chroma := val - min

	var sat float64
	if math.Abs(val) < 1e-4 {
		sat = 0.0
	} else {
		sat = chroma / val
	}

	var hue float64
	if chroma < 1e-4 {
		hue = 0.0
	} else if max == red {
		hue = 60.0 * math.Mod((green-blue)/chroma, 6.0)
		if hue < 0.0 {
			hue += 360.0
		}
	} else if max == green {
		hue = 60.0 * (2.0 + (blue-red)/chroma)
	} else {
		hue = 60.0 * (4.0 + (red-green)/chroma)
	}

	return hsv{h: hue, s: sat, v: val}
}

func hsvToRGB(h hsv) (float64, float64, float64) {
	c := h.v * h.s
	hPrime := h.h / 60.0
	x := c * (1.0 - math.Abs(math.Mod(hPrime, 2.0)-1.0))

	var r1, g1, b1 float64
	if hPrime >= 0.0 && hPrime < 1.0 {
		r1, g1, b1 = c, x, 0.0
	} else if hPrime >= 1.0 && hPrime < 2.0 {
		r1, g1, b1 = x, c, 0.0
	} else if hPrime >= 2.0 && hPrime < 3.0 {
		r1, g1, b1 = 0.0, c, x
	} else if hPrime >= 3.0 && hPrime < 4.0 {
		r1, g1, b1 = 0.0, x, c
	} else if hPrime >= 4.0 && hPrime < 5.0 {
		r1, g1, b1 = x, 0.0, c
	} else {
		r1, g1, b1 = c, 0.0, x
	}

	m := h.v - c
	return r1 + m, g1 + m, b1 + m
}

func calculateGrayscaleFromHSV(h hsv) float64 {
	return h.v * h.v
}

func getASCIIChar(grayscale float64) rune {
	nValues := len(ASCIIViewChars)
	idx := int(grayscale * float64(nValues))
	if idx >= nValues {
		idx = nValues - 1
	}
	if idx < 0 {
		idx = 0
	}
	return rune(ASCIIViewChars[idx])
}

func getSobelAngleChar(angle float64) rune {
	if (22.5 <= angle && angle <= 67.5) || (-157.5 <= angle && angle <= -112.5) {
		return '\\'
	} else if (67.5 <= angle && angle <= 112.5) || (-112.5 <= angle && angle <= -67.5) {
		return '_'
	} else if (112.5 <= angle && angle <= 157.5) || (-67.5 <= angle && angle <= -22.5) {
		return '/'
	}
	return '|'
}

func ImageToASCIIView(img image.Image, targetWidth, targetHeight int) []string {
	return RenderASCIIViewLines(img, ASCIIViewConfig{
		MaxWidth:      targetWidth,
		MaxHeight:     targetHeight,
		EdgeThreshold: EnhancedEdgeThreshold,
		CharRatio:     DefaultCharRatio,
		RetroColors:   false,
		CenterPadding: true,
	})
}

func ImageToASCIIViewCustom(img image.Image, maxWidth, maxHeight int, edgeThreshold, charRatio float64, retroColors bool) []string {
	return RenderASCIIViewLines(img, ASCIIViewConfig{
		MaxWidth:      maxWidth,
		MaxHeight:     maxHeight,
		EdgeThreshold: edgeThreshold,
		CharRatio:     charRatio,
		RetroColors:   retroColors,
		CenterPadding: true,
	})
}

func RenderASCIIViewLines(img image.Image, cfg ASCIIViewConfig) []string {
	if img == nil {
		return nil
	}

	if cfg.MaxWidth <= 0 || cfg.MaxHeight <= 0 {
		w, h := DetectTerminalSize()
		if cfg.MaxWidth <= 0 {
			cfg.MaxWidth = w
		}
		if cfg.MaxHeight <= 0 {
			cfg.MaxHeight = h
		}
	}
	if cfg.CharRatio <= 0 {
		cfg.CharRatio = DefaultCharRatio
	}
	if cfg.EdgeThreshold <= 0 {
		cfg.EdgeThreshold = DefaultEdgeThreshold
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()
	if origW <= 0 || origH <= 0 {
		return nil
	}

	var width, height int
	proposedHeight := int(float64(origH*cfg.MaxWidth) / (cfg.CharRatio * float64(origW)))
	if proposedHeight <= cfg.MaxHeight {
		width = cfg.MaxWidth
		height = proposedHeight
	} else {
		width = int(float64(origW*cfg.MaxHeight) * cfg.CharRatio / float64(origH))
		height = cfg.MaxHeight
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	redData := make([][]float64, height)
	greenData := make([][]float64, height)
	blueData := make([][]float64, height)
	grayData := make([][]float64, height)

	for j := 0; j < height; j++ {
		redData[j] = make([]float64, width)
		greenData[j] = make([]float64, width)
		blueData[j] = make([]float64, width)
		grayData[j] = make([]float64, width)

		y1 := (j * origH) / height
		y2 := ((j + 1) * origH) / height
		if y2 <= y1 {
			y2 = y1 + 1
		}
		if y2 > origH {
			y2 = origH
		}

		for i := 0; i < width; i++ {
			x1 := (i * origW) / width
			x2 := ((i + 1) * origW) / width
			if x2 <= x1 {
				x2 = x1 + 1
			}
			if x2 > origW {
				x2 = origW
			}

			var sumR, sumG, sumB float64
			nPixels := float64((x2 - x1) * (y2 - y1))

			for py := y1; py < y2; py++ {
				for px := x1; px < x2; px++ {
					c := img.At(bounds.Min.X+px, bounds.Min.Y+py)
					r, g, b, a := c.RGBA()
					if a > 0 {
						sumR += float64(r) / float64(a)
						sumG += float64(g) / float64(a)
						sumB += float64(b) / float64(a)
					}
				}
			}

			avgR := sumR / nPixels
			avgG := sumG / nPixels
			avgB := sumB / nPixels
			if avgR > 1.0 {
				avgR = 1.0
			}
			if avgG > 1.0 {
				avgG = 1.0
			}
			if avgB > 1.0 {
				avgB = 1.0
			}

			redData[j][i] = avgR
			greenData[j][i] = avgG
			blueData[j][i] = avgB

			grayData[j][i] = 0.2126*avgR + 0.7152*avgG + 0.0722*avgB
		}
	}

	sobelX := make([][]float64, height)
	sobelY := make([][]float64, height)
	for j := 0; j < height; j++ {
		sobelX[j] = make([]float64, width)
		sobelY[j] = make([]float64, width)
	}

	if cfg.EdgeThreshold < 4.0 && width >= 3 && height >= 3 {
		for y := 1; y < height-1; y++ {
			for x := 1; x < width-1; x++ {

				sobelX[y][x] = -grayData[y-1][x-1] + grayData[y-1][x+1] +
					-2.0*grayData[y][x-1] + 2.0*grayData[y][x+1] +
					-grayData[y+1][x-1] + grayData[y+1][x+1]

				sobelY[y][x] = grayData[y-1][x-1] + 2.0*grayData[y-1][x] + grayData[y-1][x+1] -
					grayData[y+1][x-1] - 2.0*grayData[y+1][x] - grayData[y+1][x+1]
			}
		}
	}

	var leftPadding string
	if cfg.CenterPadding {
		padLeft := (cfg.MaxWidth - width) / 2
		if padLeft > 0 {
			leftPadding = strings.Repeat(" ", padLeft)
		}
	}

	lines := make([]string, 0, height)
	for y := 0; y < height; y++ {
		var sb strings.Builder
		if leftPadding != "" {
			sb.WriteString(leftPadding)
		}

		for x := 0; x < width; x++ {
			sx := sobelX[y][x]
			sy := sobelY[y][x]
			squareSobelMag := sx*sx + sy*sy
			sobelAngle := math.Atan2(sy, sx) * 180.0 / math.Pi

			hVal := rgbToHSV(redData[y][x], greenData[y][x], blueData[y][x])

			grayscale := calculateGrayscaleFromHSV(hVal)

			hVal.v = 1.0

			var r, g, b int
			if cfg.RetroColors {

				hVal.h = math.Round(hVal.h/60.0) * 60.0
				if hVal.h >= 360.0 {
					hVal.h = 0.0
				}
				if hVal.s < 0.25 {
					hVal.s = 0.0
				} else {
					hVal.s = 1.0
				}
				rd, gd, bd := hsvToRGB(hVal)
				r = int(math.Round(rd * 255.0))
				g = int(math.Round(gd * 255.0))
				b = int(math.Round(bd * 255.0))
			} else {

				rd, gd, bd := hsvToRGB(hVal)
				r = int(math.Round(rd * 255.0))
				g = int(math.Round(gd * 255.0))
				b = int(math.Round(bd * 255.0))
			}

			if r < 0 {
				r = 0
			} else if r > 255 {
				r = 255
			}
			if g < 0 {
				g = 0
			} else if g > 255 {
				g = 255
			}
			if b < 0 {
				b = 0
			} else if b > 255 {
				b = 255
			}

			asciiChar := getASCIIChar(grayscale)

			if squareSobelMag >= cfg.EdgeThreshold*cfg.EdgeThreshold {
				asciiChar = getSobelAngleChar(sobelAngle)
			}

			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm%c", r, g, b, asciiChar)
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}

	return lines
}

func RenderASCIIViewToString(img image.Image, cfg ASCIIViewConfig) string {
	lines := RenderASCIIViewLines(img, cfg)
	return strings.Join(lines, "\n") + "\n"
}

func LoadImageAny(target string) (image.Image, error) {
	if target == "" {
		return nil, fmt.Errorf("empty image target")
	}

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 Rhythm-ASCII-View")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d fetching image", resp.StatusCode)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}

	f, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(target))
	audioExts := map[string]bool{
		".mp3": true, ".flac": true, ".m4a": true, ".ogg": true,
		".wav": true, ".opus": true, ".aac": true, ".wma": true,
	}

	if audioExts[ext] {
		m, err := tag.ReadFrom(f)
		if err == nil && m != nil && m.Picture() != nil && len(m.Picture().Data) > 0 {
			img, _, decodeErr := image.Decode(bytes.NewReader(m.Picture().Data))
			if decodeErr == nil && img != nil {
				return img, nil
			}
		}

		dir := filepath.Dir(target)
		coverNames := []string{"cover.jpg", "cover.png", "folder.jpg", "album.jpg", "front.jpg", "artwork.jpg"}
		for _, name := range coverNames {
			coverPath := filepath.Join(dir, name)
			if cf, err := os.Open(coverPath); err == nil {
				img, _, err := image.Decode(cf)
				cf.Close()
				if err == nil && img != nil {
					return img, nil
				}
			}
		}
	}

	_, _ = f.Seek(0, io.SeekStart)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image '%s': %w", target, err)
	}
	return img, nil
}
