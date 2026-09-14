package main

import (
	"fmt"
	"os"
	"strconv"

	"rhythm/internal/metadata"
)

func printHelp(progName string) {
	fmt.Printf("USAGE:\n")
	fmt.Printf("\t%s <path/to/image> [OPTIONS]\n\n", progName)

	fmt.Printf("ARGUMENTS:\n")
	fmt.Printf("\t<path/to/image>\t\tPath to image or audio file (JPEG, PNG, GIF, BMP, WebP, FLAC, MP3)\n\n")

	fmt.Printf("ASCII OPTIONS:\n")
	fmt.Printf("\t-mw <width>\t\tMaximum width in characters (default: terminal width OR 64)\n")
	fmt.Printf("\t-mh <height>\t\tMaximum height in characters (default: terminal height OR 48)\n")
	fmt.Printf("\t-et <threshold>\t\tEdge detection threshold, range: 0.0 - 4.0 (default: 4.0, disabled)\n")
	fmt.Printf("\t-cr <ratio>\t\tHeight-to-width ratio for characters (default: 2.0)\n")
	fmt.Printf("\t--retro-colors\t\tUse 3-bit retro color palette (8 colors) instead of 24-bit truecolor\n")
}

func main() {
	args := os.Args
	if len(args) < 2 {
		printHelp(args[0])
		os.Exit(1)
	}

	if args[1] == "-h" || args[1] == "--help" || args[1] == "help" {
		printHelp(args[0])
		return
	}

	filePath := args[1]
	cfg := metadata.DefaultConfig()

	for i := 2; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-mw":
			if i+1 < len(args) {
				i++
				if v, err := strconv.Atoi(args[i]); err == nil && v > 0 {
					cfg.MaxWidth = v
				}
			}
		case "-mh":
			if i+1 < len(args) {
				i++
				if v, err := strconv.Atoi(args[i]); err == nil && v > 0 {
					cfg.MaxHeight = v
				}
			}
		case "-et":
			if i+1 < len(args) {
				i++
				if v, err := strconv.ParseFloat(args[i], 64); err == nil && v >= 0.0 {
					cfg.EdgeThreshold = v
				}
			}
		case "-cr":
			if i+1 < len(args) {
				i++
				if v, err := strconv.ParseFloat(args[i], 64); err == nil && v > 0.0 {
					cfg.CharRatio = v
				}
			}
		case "--retro-colors":
			cfg.RetroColors = true
		default:
			fmt.Fprintf(os.Stderr, "Warning: Ignoring unrecognized argument '%s'\n", arg)
		}
	}

	img, err := metadata.LoadImageAny(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	lines := metadata.RenderASCIIViewLines(img, cfg)
	for _, line := range lines {
		fmt.Println(line)
	}
}
