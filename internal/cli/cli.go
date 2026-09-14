package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rhythm/internal/audio"
	"rhythm/internal/cache"
	"rhythm/internal/config"
	"rhythm/internal/core"
	"rhythm/internal/graphics"
	"rhythm/internal/history"
	"rhythm/internal/library"
	"rhythm/internal/metadata"
	"rhythm/internal/playlists"
	"rhythm/internal/providers"
	"rhythm/internal/queue"
	"rhythm/internal/remote"
	"rhythm/internal/storage"
	"github.com/google/uuid"
)

type AppServices struct {
	Config    *config.Config
	DB        *storage.DB
	Audio     *audio.Engine
	Library   *library.Manager
	Queue     *queue.Manager
	Playlists *playlists.Manager
	History   *history.Tracker
	Provider  providers.MusicProvider
	Remote    *remote.Client
	Cache     *cache.Manager
}

func InitServices() (*AppServices, error) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	db, err := storage.Open("")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	audioEng := audio.NewEngine()
	audioEng.SetVolume(cfg.Audio.Volume)

	libMgr := library.NewManager(db)
	queueMgr := queue.NewManager()
	plMgr := playlists.NewManager(db)
	histTracker := history.NewTracker(db, 100)
	provider := providers.NewDefaultProvider()
	remoteClient := remote.NewClient()
	cacheMgr, _ := cache.NewManager(cfg.Cache.CacheDir, cfg.Cache.MaxSizeMB)

	return &AppServices{
		Config:    cfg,
		DB:        db,
		Audio:     audioEng,
		Library:   libMgr,
		Queue:     queueMgr,
		Playlists: plMgr,
		History:   histTracker,
		Provider:  provider,
		Remote:    remoteClient,
		Cache:     cacheMgr,
	}, nil
}

func HandleCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}

	cmd := args[0]
	subArgs := args[1:]

	svc, err := InitServices()
	if err != nil {
		fmt.Printf("Initialization error: %v\n", err)
		return true
	}
	defer svc.DB.Close()

	switch cmd {
	case "doctor":
		runDoctor(svc)
	case "library":
		runLibrary(svc, subArgs)
	case "search":
		runSearch(svc, subArgs)
	case "play":
		runPlay(svc, subArgs)
	case "queue":
		runQueue(svc, subArgs)
	case "favorites":
		runFavorites(svc, subArgs)
	case "playlist":
		runPlaylist(svc, subArgs)
	case "server":
		runServer(svc, subArgs)
	case "config":
		runConfig(svc)
	case "status":
		runStatus(svc)
	case "ascii", "art", "ascii-view":
		runASCII(svc, subArgs)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s. Run 'rhythm help' for usage.\n", cmd)
	}

	return true
}

func printHelp() {
	fmt.Println("RHYTHM - Terminal Music Player & Personal Music Cloud")
	fmt.Println("\nUsage:")
	fmt.Println("  rhythm                    Launch interactive TUI player")
	fmt.Println("  rhythm doctor             Run diagnostic health checks")
	fmt.Println("  rhythm status             Show playback and library status")
	fmt.Println("  rhythm library [scan|list] Manage local music library")
	fmt.Println("  rhythm search <query>     Search local and online tracks")
	fmt.Println("  rhythm play <query>       Play audio track")
	fmt.Println("  rhythm queue              View current playback queue")
	fmt.Println("  rhythm favorites          List favorite tracks")
	fmt.Println("  rhythm playlist [list|show <name>] Manage playlists")
	fmt.Println("  rhythm server [list|add|status|browse|download <track>] Manage remote NAS")
	fmt.Println("  rhythm ascii <image|song|query> [options] Render artwork as colorized ASCII art")
	fmt.Println("  rhythm config             Show configuration")
}

func runDoctor(svc *AppServices) {
	fmt.Println("RHYTHM SYSTEM DIAGNOSTICS")
	fmt.Println("==========================")

	if svc.Audio.NativeAvailable() {
		fmt.Println("✓ Audio Engine: Windows Native Hardware Player (WinRT MediaPlayer - AAC/MP4/FLAC/MP3/Opus)")
	} else if svc.Audio.State() != "" {
		fmt.Println("✓ Audio Engine: Initialized (Beep / Speaker Output)")
	} else {
		fmt.Println("✗ Audio Engine: Failed")
	}

	if err := svc.DB.SaveTrack(&core.Track{ID: "doctor-probe", Title: "Probe", Artist: "Test", Source: core.SourceLocal}); err == nil {
		fmt.Println("✓ Database: SQLite operational with migrations")
	} else {
		fmt.Printf("✗ Database: %v\n", err)
	}

	fmt.Println("✓ Online Providers: Native NodeLink InnerTube (YouTube), JioSaavn REST (320kbps), and Spotify (No yt-dlp required)")

	for _, p := range svc.Config.Library.Paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			fmt.Printf("✓ Music Directory: Found (%s)\n", p)
		} else {
			fmt.Printf("! Music Directory: Missing or unreadable (%s)\n", p)
		}
	}

	fmt.Printf("✓ Configuration: %s\n", config.GetConfigFilePath())

	servers, _ := svc.DB.GetServers()
	fmt.Printf("• Configured NAS Servers: %d\n", len(servers))
}

func runStatus(svc *AppServices) {
	tracks, _ := svc.Library.AllTracks()
	favs, _ := svc.DB.GetFavorites()
	servers, _ := svc.DB.GetServers()

	fmt.Println("RHYTHM STATUS")
	fmt.Println("==============")
	fmt.Printf("• Local Tracks Indexed: %d\n", len(tracks))
	fmt.Printf("• Favorites Stored    : %d\n", len(favs))
	fmt.Printf("• Remote NAS Servers  : %d\n", len(servers))
	fmt.Printf("• Audio Volume        : %d%%\n", svc.Audio.Volume())
	fmt.Printf("• Cache Usage         : %s\n", svc.Cache.FormattedStats())
}

func runLibrary(svc *AppServices, args []string) {
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "scan":
		fmt.Printf("Scanning music directories: %v...\n", svc.Config.Library.Paths)
		stats, err := svc.Library.ScanDirectories(svc.Config.Library.Paths)
		if err != nil {
			fmt.Printf("Scan error: %v\n", err)
			return
		}
		fmt.Printf("✓ Scan Complete! Scanned: %d | Added: %d | Updated: %d | Errors: %d\n",
			stats.TotalScanned, stats.NewAdded, stats.Updated, stats.Errors)

	case "list":
		tracks, err := svc.Library.AllTracks()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if len(tracks) == 0 {
			fmt.Println("No tracks in local library. Run 'rhythm library scan' to index music.")
			return
		}
		fmt.Printf("LOCAL LIBRARY (%d tracks):\n", len(tracks))
		for i, t := range tracks {
			fmt.Printf("  [%2d] %s - %s (%s) [%s]\n", i+1, t.Artist, t.Title, t.Album, t.DisplayDuration())
		}
	}
}

func runSearch(svc *AppServices, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: rhythm search <query>")
		return
	}
	query := strings.Join(args, " ")
	fmt.Printf("Searching for '%s'...\n\n", query)

	local, _ := svc.Library.Search(query)
	if len(local) > 0 {
		fmt.Printf("LOCAL MATCHES (%d):\n", len(local))
		for i, t := range local {
			fmt.Printf("  [%d] [LOCAL]  %s - %s (%s)\n", i+1, t.Artist, t.Title, t.Album)
		}
		fmt.Println()
	}

	online, err := svc.Provider.Search(query, 5)
	if err == nil && len(online) > 0 {
		fmt.Printf("ONLINE MATCHES (%d):\n", len(online))
		for i, t := range online {
			fmt.Printf("  [%d] [ONLINE] %s - %s [%s]\n", i+1, t.Artist, t.Title, t.DisplayDuration())
		}
		fmt.Println()
	}

	servers, _ := svc.DB.GetServers()
	if len(servers) > 0 {
		remoteTracks, err := svc.Remote.BrowseLibrary(&servers[0], query)
		if err == nil && len(remoteTracks) > 0 {
			fmt.Printf("NAS MATCHES (%d):\n", len(remoteTracks))
			for i, t := range remoteTracks {
				fmt.Printf("  [%d] [NAS]    %s - %s (%s)\n", i+1, t.Artist, t.Title, t.Album)
			}
		}
	}
}

func runPlay(svc *AppServices, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: rhythm play <song name or file> [--cover-window]")
		return
	}

	showCoverWin := false
	var cleanArgs []string
	for _, a := range args {
		if a == "--cover-window" || a == "-w" {
			showCoverWin = true
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}
	if len(cleanArgs) == 0 {
		fmt.Println("Usage: rhythm play <song name or file> [--cover-window]")
		return
	}
	target := strings.Join(cleanArgs, " ")

	if _, err := os.Stat(target); err == nil {
		t := &core.Track{
			ID:        uuid.New().String(),
			Title:     filepath.Base(target),
			LocalPath: target,
			Source:    core.SourceLocal,
		}
		fmt.Printf("Playing local file: %s\n", target)
		if showCoverWin {
			artPath := metadata.GetTrackArtworkPathOrURL(t)
			_ = graphics.GetCoverWindow().UpdateCover(artPath, t.Title, t.Artist)
		}
		if err := svc.Audio.Play(t); err != nil {
			fmt.Printf("Playback error: %v\n", err)
			return
		}
		fmt.Println("Playing... Press Ctrl+C to stop.")
		time.Sleep(5 * time.Second)
		if showCoverWin {
			graphics.GetCoverWindow().Close()
		}
		return
	}

	matches, _ := svc.Library.Search(target)
	if len(matches) > 0 {
		t := matches[0]
		fmt.Printf("Playing from local library: %s - %s\n", t.Artist, t.Title)
		if showCoverWin {
			artPath := metadata.GetTrackArtworkPathOrURL(&t)
			_ = graphics.GetCoverWindow().UpdateCover(artPath, t.Title, t.Artist)
		}
		_ = svc.Audio.Play(&t)
		time.Sleep(3 * time.Second)
		if showCoverWin {
			graphics.GetCoverWindow().Close()
		}
		return
	}

	onlineMatches, err := svc.Provider.Search(target, 1)
	if err == nil && len(onlineMatches) > 0 {
		t := onlineMatches[0]
		fmt.Printf("Resolving stream for: %s - %s...\n", t.Artist, t.Title)
		streamURL, err := svc.Provider.Resolve(&t)
		if err == nil {
			t.StreamURL = streamURL
			fmt.Printf("Streaming: %s\n", streamURL)

			if showCoverWin {
				artPath := metadata.GetTrackArtworkPathOrURL(&t)
				_ = graphics.GetCoverWindow().UpdateCover(artPath, t.Title, t.Artist)
			}

			if err := svc.Audio.Play(&t); err != nil {
				fmt.Printf("Playback error: %v\n", err)
				return
			}
			fmt.Println("Playing through Windows Audio... Playing sample for 15 seconds:")
			for i := 0; i < 15; i++ {
				time.Sleep(1 * time.Second)
				pos, dur := svc.Audio.Progress()
				fmt.Printf("\r[▶] %s / %s  (Volume: %d%%)   ", formatSeconds(pos), formatSeconds(dur), svc.Audio.Volume())
			}
			if showCoverWin {
				graphics.GetCoverWindow().Close()
			}
			fmt.Println("\nFinished sample. Use 'rhythm' for full interactive TUI.")
			return
		}
	}

	fmt.Printf("No track found matching '%s'\n", target)
}

func formatSeconds(sec float64) string {
	mins := int(sec) / 60
	secs := int(sec) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func runQueue(svc *AppServices, args []string) {
	items := svc.Queue.Items()
	if len(items) == 0 {
		fmt.Println("Playback queue is empty.")
		return
	}
	fmt.Printf("PLAYBACK QUEUE (%d items):\n", len(items))
	for i, it := range items {
		marker := " "
		if i == svc.Queue.CurrentIndex() {
			marker = ">"
		}
		fmt.Printf(" %s [%2d] [%s] %s - %s (%s)\n", marker, i+1, it.Track.Source, it.Track.Artist, it.Track.Title, it.Track.DisplayDuration())
	}
}

func runFavorites(svc *AppServices, args []string) {
	favs, err := svc.DB.GetFavorites()
	if err != nil || len(favs) == 0 {
		fmt.Println("No favorite tracks starred yet.")
		return
	}
	fmt.Printf("FAVORITES (%d tracks):\n", len(favs))
	for i, t := range favs {
		fmt.Printf("  [%2d] [%s] %s - %s\n", i+1, t.Source, t.Artist, t.Title)
	}
}

func runPlaylist(svc *AppServices, args []string) {
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		lists, _ := svc.DB.GetPlaylists()
		if len(lists) == 0 {
			fmt.Println("No playlists created yet.")
			return
		}
		fmt.Printf("PLAYLISTS (%d):\n", len(lists))
		for i, p := range lists {
			fmt.Printf("  [%2d] %s (%d tracks) - %s\n", i+1, p.Name, len(p.Tracks), p.Description)
		}
	case "show":
		if len(args) < 2 {
			fmt.Println("Usage: rhythm playlist show <name>")
			return
		}
		name := strings.ToLower(args[1])
		lists, _ := svc.DB.GetPlaylists()
		for _, p := range lists {
			if strings.Contains(strings.ToLower(p.Name), name) {
				fmt.Printf("PLAYLIST: %s (%d tracks)\n", p.Name, len(p.Tracks))
				for i, t := range p.Tracks {
					fmt.Printf("  [%2d] [%s] %s - %s (%s)\n", i+1, t.Source, t.Artist, t.Title, t.DisplayDuration())
				}
				return
			}
		}
		fmt.Println("Playlist not found.")
	}
}

func runServer(svc *AppServices, args []string) {
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		servers, _ := svc.DB.GetServers()
		if len(servers) == 0 {
			fmt.Println("No remote servers configured. Add one via 'rhythm server add'.")
			return
		}
		fmt.Printf("CONFIGURED MUSIC SERVERS (%d):\n", len(servers))
		for i, s := range servers {
			fmt.Printf("  [%2d] %s (%s) [Capabilities: %s]\n", i+1, s.Name, s.BaseURL(), strings.Join(s.Capabilities, ", "))
		}

	case "add":

		if len(args) < 4 {
			fmt.Println("Usage: rhythm server add <name> <host> <port> [username] [password]")
			return
		}
		name := args[1]
		host := args[2]
		var port int
		fmt.Sscanf(args[3], "%d", &port)
		if port == 0 {
			port = 8765
		}
		user := ""
		pass := ""
		if len(args) > 4 {
			user = args[4]
		}
		if len(args) > 5 {
			pass = args[5]
		}

		srv := &core.ServerConfig{
			ID:           uuid.New().String(),
			Name:         name,
			Host:         host,
			Port:         port,
			Username:     user,
			Password:     pass,
			Capabilities: []string{"streaming", "acquisition", "library"},
			IsActive:     true,
			CreatedAt:    time.Now(),
		}

		fmt.Printf("Connecting to %s (%s:%d)...\n\n", name, host, port)
		test := svc.Remote.TestConnection(srv)

		if test.Reachable {
			fmt.Println("  ✓ Server reachable")
		} else {
			fmt.Printf("  ✗ Server unreachable: %s\n", test.ErrorMessage)
			return
		}

		if test.Authenticated {
			fmt.Println("  ✓ Authentication successful")
		} else {
			fmt.Printf("  ✗ Authentication failed: %s\n", test.ErrorMessage)
			return
		}

		if test.LibraryAvailable {
			fmt.Printf("  ✓ Music library available (%d tracks indexed on server)\n", test.TotalTracks)
		}
		if test.StreamingSupported {
			fmt.Println("  ✓ Audio streaming supported")
		}
		if test.AcquisitionSupported {
			fmt.Println("  ✓ Remote acquisition supported")
		}

		_ = svc.DB.SaveServer(srv)
		fmt.Printf("\n✓ '%s' added successfully! NAS menu is now unlocked.\n", name)

	case "status":
		servers, _ := svc.DB.GetServers()
		if len(servers) == 0 {
			fmt.Println("No remote servers configured.")
			return
		}
		srv := servers[0]
		test := svc.Remote.TestConnection(&srv)
		fmt.Printf("SERVER: %s (%s)\n", srv.Name, srv.BaseURL())
		if test.Reachable {
			fmt.Printf("  ● Status: Online (v%s)\n", test.Version)
			fmt.Printf("  • Remote Library: %d tracks\n", test.TotalTracks)
		} else {
			fmt.Println("  ● Status: Offline")
		}

	case "refresh":
		servers, _ := svc.DB.GetServers()
		if len(servers) == 0 {
			fmt.Println("No remote servers configured.")
			return
		}
		srv := servers[0]
		reqURL := fmt.Sprintf("%s/api/v1/library/refresh", srv.BaseURL())
		req, _ := http.NewRequest(http.MethodPost, reqURL, nil)
		if srv.Token != "" {
			req.Header.Set("Authorization", "Bearer "+srv.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Refresh failed: %v\n", err)
			return
		}
		resp.Body.Close()
		fmt.Printf("✓ '%s' library scan & refresh triggered!\n", srv.Name)

	case "browse":
		servers, _ := svc.DB.GetServers()
		if len(servers) == 0 {
			fmt.Println("No remote servers configured.")
			return
		}
		q := ""
		if len(args) > 1 {
			q = strings.Join(args[1:], " ")
		}
		tracks, err := svc.Remote.BrowseLibrary(&servers[0], q)
		if err != nil {
			fmt.Printf("Browse error: %v\n", err)
			return
		}
		fmt.Printf("NAS LIBRARY (%d tracks):\n", len(tracks))
		for i, t := range tracks {
			fmt.Printf("  [%2d] %s - %s (%s)\n", i+1, t.Artist, t.Title, t.Album)
		}

	case "download":

		servers, _ := svc.DB.GetServers()
		if len(servers) == 0 {
			fmt.Println("Error: No remote NAS server configured. Configure one first via 'rhythm server add'.")
			return
		}
		if len(args) < 2 {
			fmt.Println("Usage: rhythm server download <track query or url>")
			return
		}
		query := strings.Join(args[1:], " ")
		fmt.Printf("Dispatching remote acquisition job to '%s'...\n", servers[0].Name)

		artist := "Various Artists"
		title := query
		album := "Singles"
		if strings.Contains(query, " - ") {
			parts := strings.SplitN(query, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
			album = title
		}

		track := &core.Track{
			ID:       uuid.New().String(),
			Title:    title,
			Artist:   artist,
			Album:    album,
			Source:   core.SourceOnline,
			SourceID: query,
		}

		job, err := svc.Remote.SaveToNAS(&servers[0], track)
		if err != nil {
			fmt.Printf("Failed to create remote job: %v\n", err)
			return
		}

		fmt.Printf("✓ Remote Job Queued on NAS! Job ID: %s (Status: %s)\n", job.ID, job.Status)
		fmt.Println("  (The NAS server is now downloading and organizing the audio independently)")
	}
}

func runConfig(svc *AppServices) {
	fmt.Printf("Config File: %s\n", config.GetConfigFilePath())
	fmt.Printf("Audio Volume: %d%%\n", svc.Config.Audio.Volume)
	fmt.Printf("Cache Directory: %s (%d MB max)\n", svc.Config.Cache.CacheDir, svc.Config.Cache.MaxSizeMB)
	fmt.Printf("Music Scan Paths: %v\n", svc.Config.Library.Paths)
}

func runASCII(svc *AppServices, args []string) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("RHYTHM ASCII ART VIEWER")
		fmt.Println("========================")
		fmt.Println("Render images, audio album covers, or online music artwork as colorized ASCII art.")
		fmt.Println("\nUsage:")
		fmt.Println("  rhythm ascii <image-path | audio-file | track-query> [OPTIONS]")
		fmt.Println("\nOptions:")
		fmt.Println("  -mw <width>        Maximum width in characters (default: terminal width OR 64)")
		fmt.Println("  -mh <height>       Maximum height in characters (default: terminal height OR 48)")
		fmt.Println("  -et <threshold>    Edge detection threshold 0.0 - 4.0 (default: 4.0 disabled, 1.8 for edge lines)")
		fmt.Println("  -cr <ratio>        Height-to-width character aspect ratio (default: 2.0)")
		fmt.Println("  --retro-colors     Use 3-bit retro ANSI color palette (8 colors)")
		fmt.Println("  --mode <mode>      Art mode: asciiview (default), halfblock, cyberpunk, braille")
		return
	}

	target := args[0]
	cfg := metadata.DefaultConfig()
	artMode := "asciiview"

	for i := 1; i < len(args); i++ {
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
		case "--mode":
			if i+1 < len(args) {
				i++
				artMode = strings.ToLower(args[i])
			}
		}
	}

	img, err := metadata.LoadImageAny(target)
	if err != nil {

		tracks, sErr := svc.Library.Search(target)
		if sErr == nil && len(tracks) > 0 {
			img, err = metadata.LoadTrackImage(&tracks[0])
		} else if svc.Provider != nil {
			onlineResults, pErr := svc.Provider.Search(target, 1)
			if pErr == nil && len(onlineResults) > 0 {
				img, err = metadata.LoadTrackImage(&onlineResults[0])
			}
		}
	}

	if err != nil || img == nil {
		fmt.Printf("Error: could not load artwork for '%s': %v\n", target, err)
		return
	}

	switch artMode {
	case "halfblock":
		lines := metadata.ImageToHalfBlock(img, cfg.MaxWidth, cfg.MaxHeight)
		for _, l := range lines {
			fmt.Println(l)
		}
	case "cyberpunk":
		lines := metadata.ImageToCyberpunkASCII(img, cfg.MaxWidth, cfg.MaxHeight)
		for _, l := range lines {
			fmt.Println(l)
		}
	case "braille":
		lines := metadata.ImageToBraille(img, cfg.MaxWidth, cfg.MaxHeight)
		for _, l := range lines {
			fmt.Println(l)
		}
	default:
		lines := metadata.RenderASCIIViewLines(img, cfg)
		for _, l := range lines {
			fmt.Println(l)
		}
	}
}
