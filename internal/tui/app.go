package tui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"rhythm/internal/cli"
	"rhythm/internal/core"
	"rhythm/internal/graphics"
	"rhythm/internal/lyrics"
	"rhythm/internal/metadata"
	"rhythm/internal/providers"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ViewMode int

const (
	ViewHome ViewMode = iota
	ViewLibrary
	ViewOnline
	ViewNAS
	ViewPlaylists
	ViewFavorites
	ViewQueue
	ViewDownloads
	ViewSettings
)

type Model struct {
	svc         *cli.AppServices
	currentView ViewMode
	cursor      int
	width       int
	height      int
	statusMsg   string
	statusTime  time.Time
	animFrame   int

	libraryTracks   []core.Track
	onlineTracks    []core.Track
	nasTracks       []core.Track
	onlineQuery     string
	searching       bool
	searchBuffer    string
	remoteJobs      []core.AcquisitionJob
	streamStatus    string
	pendingStreamID string

	unifiedQuery       string
	unifiedTracks      []core.Track
	unifiedAll         []core.Track
	unifiedLocal       []core.Track
	unifiedOnline      []core.Track
	searchFilter       int
	isSearchingUnified bool

	currentLyrics      *lyrics.Lyrics
	loadingLyricsTrack string

	addingServer    bool
	serverInputStep int
	newServerName   string
	newServerHost   string
	newServerPort   string
	newServerUser   string
	newServerPass   string
}

type tickMsg time.Time

type unifiedSearchMsg struct {
	query        string
	localTracks  []core.Track
	onlineTracks []core.Track
	allTracks    []core.Track
	err          error
}

type streamResolvedMsg struct {
	track core.Track
	err   error
}

func tickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type lyricsLoadedMsg struct {
	trackID string
	title   string
	artist  string
	lyrics  *lyrics.Lyrics
}

func loadLyricsCmd(track *core.Track) tea.Cmd {
	return func() tea.Msg {
		if track == nil {
			return nil
		}
		l, err := lyrics.GetLyrics(track)
		if err != nil || l == nil {
			return nil
		}
		return lyricsLoadedMsg{trackID: track.ID, title: track.Title, artist: track.Artist, lyrics: l}
	}
}

func unifiedSearchCmd(svc *cli.AppServices, query string) tea.Cmd {
	return func() tea.Msg {
		clean := strings.TrimSpace(query)
		if clean == "" {
			return unifiedSearchMsg{}
		}

		var local []core.Track
		lowerQ := strings.ToLower(clean)
		if all, err := svc.Library.AllTracks(); err == nil {
			for _, t := range all {
				if strings.Contains(strings.ToLower(t.Title), lowerQ) ||
					strings.Contains(strings.ToLower(t.Artist), lowerQ) ||
					strings.Contains(strings.ToLower(t.Album), lowerQ) {
					t.Source = core.SourceLocal
					if t.RemoteReference == "" {
						t.RemoteReference = "Local"
					}
					local = append(local, t)
				}
			}
		}

		var online []core.Track
		if comp, ok := svc.Provider.(*providers.CompositeProvider); ok {
			online = comp.SearchUnified(clean, 15)
		} else {
			online, _ = svc.Provider.Search(clean, 15)
		}

		var all []core.Track
		all = append(all, local...)
		all = append(all, online...)

		return unifiedSearchMsg{
			query:        clean,
			localTracks:  local,
			onlineTracks: online,
			allTracks:    all,
		}
	}
}

func InitialModel(svc *cli.AppServices) Model {
	tracks, _ := svc.Library.AllTracks()
	m := Model{
		svc:           svc,
		currentView:   ViewHome,
		cursor:        0,
		width:         110,
		height:        35,
		libraryTracks: tracks,
		statusMsg:     "Ready. Press [/] to search all sources or type /setting",
		statusTime:    time.Now(),
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.animFrame = (m.animFrame + 1) % 100
		if m.currentView == ViewDownloads {
			servers, _ := m.svc.DB.GetServers()
			if len(servers) > 0 {
				jobs, _ := m.svc.Remote.GetJobs(&servers[0])
				m.remoteJobs = jobs
			}
		}
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd())

		if cur := m.svc.Audio.CurrentTrack(); cur != nil {
			if (m.currentLyrics == nil || m.currentLyrics.TrackID != cur.ID) && m.loadingLyricsTrack != cur.ID {
				m.loadingLyricsTrack = cur.ID
				cmds = append(cmds, loadLyricsCmd(cur))
			}
		}

		if len(cmds) > 1 {
			return m, tea.Batch(cmds...)
		}
		return m, tickCmd()

	case lyricsLoadedMsg:
		m.loadingLyricsTrack = ""
		cur := m.svc.Audio.CurrentTrack()
		if cur != nil && (cur.ID == msg.trackID || cur.Title == msg.title) {
			m.currentLyrics = msg.lyrics
		}
		return m, nil

	case unifiedSearchMsg:
		m.isSearchingUnified = false
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Search error: %v", msg.err))
		} else {
			m.unifiedQuery = msg.query
			m.unifiedAll = msg.allTracks
			m.unifiedLocal = msg.localTracks
			m.unifiedOnline = msg.onlineTracks
			m.updateUnifiedFilteredList()
			m.cursor = 0
			if len(msg.allTracks) > 0 {
				m.setStatus(fmt.Sprintf("✓ Found %d results (%d Local, %d Online)! [Enter] Play, [Tab] Filter, [a] Queue",
					len(msg.allTracks), len(msg.localTracks), len(msg.onlineTracks)))
			} else {
				m.setStatus(fmt.Sprintf("No results found for '%s'", msg.query))
			}
		}
		return m, nil

	case streamResolvedMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Playback error: %v", msg.err))
		} else {
			m.setStatus(fmt.Sprintf("▶ Now Playing: %s - %s", msg.track.Artist, msg.track.Title))
			if graphics.GetCoverWindow().IsVisible() {
				artPath := metadata.GetTrackArtworkPathOrURL(&msg.track)
				_ = graphics.GetCoverWindow().UpdateCover(artPath, msg.track.Title, msg.track.Artist)
			}
			var cmds []tea.Cmd
			if (m.currentLyrics == nil || m.currentLyrics.TrackID != msg.track.ID) && m.loadingLyricsTrack != msg.track.ID {
				m.loadingLyricsTrack = msg.track.ID
				cmds = append(cmds, loadLyricsCmd(&msg.track))
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.handleSearchInput(msg)
		}
		if m.addingServer {
			return m.handleServerInput(msg)
		}

		key := msg.String()
		switch key {
		case "ctrl+c":
			graphics.GetCoverWindow().Close()
			return m, tea.Quit

		case "esc":
			if m.unifiedQuery != "" {
				m.unifiedQuery = ""
				m.unifiedAll = nil
				m.unifiedTracks = nil
				m.searchFilter = 0
				m.cursor = 0
				m.setStatus("Search cleared. Showing Local Library.")
				return m, nil
			}
			if m.currentView != ViewHome {
				m.currentView = ViewHome
				m.cursor = 0
				return m, nil
			}
			return m, tea.Quit

		case "tab":
			if m.unifiedQuery != "" && len(m.unifiedAll) > 0 {
				m.searchFilter = (m.searchFilter + 1) % 5
				m.updateUnifiedFilteredList()
				names := []string{"All", "Local", "YouTube", "Spotify", "JioSaavn"}
				m.setStatus(fmt.Sprintf("Filter: [%s] (%d tracks)", names[m.searchFilter], len(m.unifiedTracks)))
				return m, nil
			}

		case "1":
			m.currentView = ViewHome
			m.cursor = 0
		case "2":
			m.currentView = ViewLibrary
			m.cursor = 0
			m.libraryTracks, _ = m.svc.Library.AllTracks()
		case "3":
			m.currentView = ViewOnline
			m.cursor = 0
		case "4":
			servers, _ := m.svc.DB.GetServers()
			if len(servers) > 0 {
				m.currentView = ViewNAS
				m.cursor = 0
				m.nasTracks, _ = m.svc.Remote.BrowseLibrary(&servers[0], "")
			} else {
				m.setStatus("NAS not configured. Press 9 or type /setting to add server.")
			}
		case "5":
			m.currentView = ViewPlaylists
			m.cursor = 0
		case "6":
			m.currentView = ViewFavorites
			m.cursor = 0
		case "7":
			m.currentView = ViewQueue
			m.cursor = 0
		case "8":
			m.currentView = ViewDownloads
			m.cursor = 0
			servers, _ := m.svc.DB.GetServers()
			if len(servers) > 0 {
				m.remoteJobs, _ = m.svc.Remote.GetJobs(&servers[0])
			}
		case "9":
			m.currentView = ViewSettings
			m.cursor = 0

		case " ":
			m.svc.Audio.TogglePlayPause()
		case "n", ">":
			if next := m.svc.Queue.Next(); next != nil {
				_ = m.svc.Audio.Play(next)
				_ = m.svc.History.Record(*next, 0)
			}
		case "p", "<":
			if prev := m.svc.Queue.Previous(); prev != nil {
				_ = m.svc.Audio.Play(prev)
			}
		case "+", "=":
			v := m.svc.Audio.AdjustVolume(5)
			m.setStatus(fmt.Sprintf("Volume: %d%%", v))
		case "-":
			v := m.svc.Audio.AdjustVolume(-5)
			m.setStatus(fmt.Sprintf("Volume: %d%%", v))
		case "right":
			m.svc.Audio.Seek(10)
		case "left":
			m.svc.Audio.Seek(-10)

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			maxLen := m.currentListLength()
			if m.cursor < maxLen-1 {
				m.cursor++
			}

		case "enter":
			return m, m.handleSelection()

		case "a":
			if t := m.getSelectedTrack(); t != nil {
				m.svc.Queue.Add(*t)
				m.setStatus(fmt.Sprintf("Added to queue: %s", t.Title))
			}

		case "f":
			if t := m.getSelectedTrack(); t != nil {
				if m.svc.DB.IsFavorite(t.ID) {
					_ = m.svc.DB.RemoveFavorite(t.ID)
					m.setStatus(fmt.Sprintf("Removed from favorites: %s", t.Title))
				} else {
					_ = m.svc.DB.AddFavorite(t)
					m.setStatus(fmt.Sprintf("Added to favorites: %s", t.Title))
				}
			}

		case "N":
			m.handleSaveToNAS()

		case "/", "s", ":":
			m.searching = true
			m.searchBuffer = ""

		case "S":
			shuf := m.svc.Queue.ToggleShuffle()
			if shuf {
				m.setStatus("Shuffle: ON")
			} else {
				m.setStatus("Shuffle: OFF")
			}
		case "r":
			mode := m.svc.Queue.CycleRepeat()
			m.setStatus(fmt.Sprintf("Repeat: %s", mode))

		case "w", "W":
			vis := graphics.GetCoverWindow().Toggle()
			if vis {
				m.setStatus("🖼️ Desktop Cover Window: SHOWN (Real Image)")
				track := m.getSelectedTrack()
				if track == nil {
					track = m.svc.Audio.CurrentTrack()
				}
				if track != nil {
					artPath := metadata.GetTrackArtworkPathOrURL(track)
					_ = graphics.GetCoverWindow().UpdateCover(artPath, track.Title, track.Artist)
				}
			} else {
				m.setStatus("🖼️ Desktop Cover Window: HIDDEN")
			}
			return m, nil

		case "?":
			m.setStatus("Hotkeys: [/] Search, [Space] Pause, [Enter] Play, [1-9] Tabs")
		}
	}

	return m, nil
}

func (m *Model) setStatus(msg string) {
	m.statusMsg = msg
	m.statusTime = time.Now()
}

func (m *Model) updateUnifiedFilteredList() {
	if len(m.unifiedAll) == 0 {
		m.unifiedTracks = nil
		m.cursor = 0
		return
	}

	switch m.searchFilter {
	case 1:
		m.unifiedTracks = m.unifiedLocal
	case 2:
		var list []core.Track
		for _, t := range m.unifiedOnline {
			if t.RemoteReference == "YouTube" {
				list = append(list, t)
			}
		}
		m.unifiedTracks = list
	case 3:
		var list []core.Track
		for _, t := range m.unifiedOnline {
			if t.RemoteReference == "Spotify" {
				list = append(list, t)
			}
		}
		m.unifiedTracks = list
	case 4:
		var list []core.Track
		for _, t := range m.unifiedOnline {
			if t.RemoteReference == "JioSaavn" {
				list = append(list, t)
			}
		}
		m.unifiedTracks = list
	default:
		m.unifiedTracks = m.unifiedAll
	}

	if m.cursor >= len(m.unifiedTracks) {
		m.cursor = 0
	}
}

func (m *Model) currentListLength() int {
	if m.unifiedQuery != "" && len(m.unifiedTracks) > 0 && (m.currentView == ViewHome || m.currentView == ViewOnline || m.currentView == ViewLibrary) {
		return len(m.unifiedTracks)
	}

	switch m.currentView {
	case ViewLibrary:
		return len(m.libraryTracks)
	case ViewOnline:
		if len(m.unifiedTracks) > 0 {
			return len(m.unifiedTracks)
		}
		return len(m.onlineTracks)
	case ViewNAS:
		return len(m.nasTracks)
	case ViewFavorites:
		favs, _ := m.svc.DB.GetFavorites()
		return len(favs)
	case ViewQueue:
		return m.svc.Queue.Len()
	case ViewDownloads:
		return len(m.remoteJobs)
	case ViewPlaylists:
		lists, _ := m.svc.DB.GetPlaylists()
		return len(lists)
	default:
		if len(m.unifiedTracks) > 0 {
			return len(m.unifiedTracks)
		}
		return len(m.libraryTracks)
	}
}

func (m *Model) getSelectedTrack() *core.Track {
	if m.unifiedQuery != "" && len(m.unifiedTracks) > 0 && (m.currentView == ViewHome || m.currentView == ViewOnline || m.currentView == ViewLibrary) {
		if m.cursor >= 0 && m.cursor < len(m.unifiedTracks) {
			return &m.unifiedTracks[m.cursor]
		}
		return nil
	}

	switch m.currentView {
	case ViewLibrary:
		if m.cursor >= 0 && m.cursor < len(m.libraryTracks) {
			return &m.libraryTracks[m.cursor]
		}
	case ViewOnline:
		if len(m.unifiedTracks) > 0 && m.cursor >= 0 && m.cursor < len(m.unifiedTracks) {
			return &m.unifiedTracks[m.cursor]
		}
		if m.cursor >= 0 && m.cursor < len(m.onlineTracks) {
			return &m.onlineTracks[m.cursor]
		}
	case ViewNAS:
		if m.cursor >= 0 && m.cursor < len(m.nasTracks) {
			return &m.nasTracks[m.cursor]
		}
	case ViewFavorites:
		favs, _ := m.svc.DB.GetFavorites()
		if m.cursor >= 0 && m.cursor < len(favs) {
			return &favs[m.cursor]
		}
	default:
		if len(m.unifiedTracks) > 0 && m.cursor >= 0 && m.cursor < len(m.unifiedTracks) {
			return &m.unifiedTracks[m.cursor]
		}
		if len(m.libraryTracks) > 0 && m.cursor >= 0 && m.cursor < len(m.libraryTracks) {
			return &m.libraryTracks[m.cursor]
		}
	}
	return nil
}

func (m *Model) handleSelection() tea.Cmd {
	if m.currentView == ViewSettings && m.cursor == 0 {
		m.addingServer = true
		m.serverInputStep = 0
		m.newServerName = "Home NAS"
		m.newServerHost = "127.0.0.1"
		m.newServerPort = "8765"
		m.newServerUser = ""
		m.newServerPass = ""
		m.searchBuffer = ""
		return nil
	}

	t := m.getSelectedTrack()
	if t == nil {
		return nil
	}

	trackToPlay := *t
	m.setStatus(fmt.Sprintf("Loading '%s'...", trackToPlay.Title))

	return func() tea.Msg {
		if trackToPlay.Source == core.SourceOnline && trackToPlay.StreamURL == "" && trackToPlay.LocalPath == "" {
			url, err := m.svc.Provider.Resolve(&trackToPlay)
			if err == nil && url != "" {
				if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
					trackToPlay.StreamURL = url
				} else {
					trackToPlay.LocalPath = url
				}
			} else {
				return streamResolvedMsg{track: trackToPlay, err: err}
			}
		}

		err := m.svc.Audio.Play(&trackToPlay)
		if err == nil {
			_ = m.svc.History.Record(trackToPlay, 0)
		}
		return streamResolvedMsg{track: trackToPlay, err: err}
	}
}

func (m *Model) handleSaveToNAS() {
	servers, _ := m.svc.DB.GetServers()
	if len(servers) == 0 {
		m.setStatus("⚠️ No NAS configured! Press 9 or type /setting to add server.")
		return
	}

	target := m.getSelectedTrack()
	if target == nil {
		target = m.svc.Audio.CurrentTrack()
	}

	if target == nil {
		m.setStatus("Select a track to save to NAS.")
		return
	}

	srv := servers[0]
	m.setStatus(fmt.Sprintf("Sending authenticated save request to %s...", srv.Name))

	go func(s core.ServerConfig, t core.Track) {
		job, err := m.svc.Remote.SaveToNAS(&s, &t)
		if err != nil {
			m.setStatus(fmt.Sprintf("✗ Save to NAS failed: %v", err))
			return
		}
		m.setStatus(fmt.Sprintf("✓ Remote Job Queued on %s! NAS is acquiring '%s'", s.Name, t.Title))
		_ = job
	}(srv, *target)
}

func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		raw := strings.TrimSpace(m.searchBuffer)
		if raw == "" {
			return m, nil
		}

		cmd := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(raw, "/"), ":")))
		switch cmd {
		case "setting", "settings", "set", "config":
			m.currentView = ViewSettings
			m.cursor = 0
			m.searchBuffer = ""
			m.setStatus("Opened Settings (Add Server, Volume, Storage)")
			return m, nil
		case "home", "h":
			m.currentView = ViewHome
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "library", "lib":
			m.currentView = ViewLibrary
			m.cursor = 0
			m.searchBuffer = ""
			m.libraryTracks, _ = m.svc.Library.AllTracks()
			return m, nil
		case "online", "search":
			m.currentView = ViewOnline
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "nas":
			servers, _ := m.svc.DB.GetServers()
			if len(servers) > 0 {
				m.currentView = ViewNAS
				m.cursor = 0
				m.nasTracks, _ = m.svc.Remote.BrowseLibrary(&servers[0], "")
			} else {
				m.setStatus("NAS not configured. Go to /setting to add server.")
			}
			m.searchBuffer = ""
			return m, nil
		case "playlists", "playlist":
			m.currentView = ViewPlaylists
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "favorites", "fav":
			m.currentView = ViewFavorites
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "queue", "q":
			m.currentView = ViewQueue
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "downloads", "jobs":
			m.currentView = ViewDownloads
			m.cursor = 0
			m.searchBuffer = ""
			return m, nil
		case "help":
			m.setStatus("Commands: /setting, /library, /online, /nas, /queue, /playlists, /favorites")
			m.searchBuffer = ""
			return m, nil
		case "quit", "exit":
			return m, tea.Quit
		}

		cleanQuery := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(raw, "/"), ":"))
		if cleanQuery != "" {
			m.unifiedQuery = cleanQuery
			m.isSearchingUnified = true
			m.searchBuffer = ""
			m.searchFilter = 0
			m.setStatus(fmt.Sprintf("Searching across Local Library, YouTube, Spotify & JioSaavn for '%s'...", cleanQuery))
			return m, unifiedSearchCmd(m.svc, cleanQuery)
		}

	case "esc":
		m.searching = false
		m.searchBuffer = ""

	case "backspace":
		if len(m.searchBuffer) > 0 {
			m.searchBuffer = m.searchBuffer[:len(m.searchBuffer)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.searchBuffer += msg.String()
		}
	}
	return m, nil
}

func (m Model) handleServerInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.addingServer = false
	case "enter":
		val := strings.TrimSpace(m.searchBuffer)
		switch m.serverInputStep {
		case 0:
			if val != "" {
				m.newServerName = val
			}
			m.serverInputStep++
			m.searchBuffer = ""
		case 1:
			if val != "" {
				m.newServerHost = val
			}
			m.serverInputStep++
			m.searchBuffer = ""
		case 2:
			if val != "" {
				m.newServerPort = val
			}
			m.serverInputStep++
			m.searchBuffer = ""
		case 3:
			m.newServerUser = val
			m.serverInputStep++
			m.searchBuffer = ""
		case 4:
			m.newServerPass = val
			m.addingServer = false
			m.searchBuffer = ""

			port := 8765
			fmt.Sscanf(m.newServerPort, "%d", &port)

			srv := core.ServerConfig{
				Name:     m.newServerName,
				Host:     m.newServerHost,
				Port:     port,
				Username: m.newServerUser,
				Password: m.newServerPass,
				Token:    m.newServerPass,
				UseTLS:   false,
			}

			go func(s core.ServerConfig) {
				m.setStatus(fmt.Sprintf("Testing live connection to %s (%s)...", s.Name, s.BaseURL()))
				test := m.svc.Remote.TestConnection(&s)
				if !test.Reachable {
					m.setStatus(fmt.Sprintf("✗ Server %s unreachable: %s", s.Name, test.ErrorMessage))
					return
				}
				_ = m.svc.DB.SaveServer(&s)
				m.setStatus(fmt.Sprintf("✓ Server '%s' connected & verified! NAS menu is now unlocked.", s.Name))
			}(srv)
		}
	case "backspace":
		if len(m.searchBuffer) > 0 {
			m.searchBuffer = m.searchBuffer[:len(m.searchBuffer)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.searchBuffer += msg.String()
		}
	}
	return m, nil
}

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EEEEEE")).
			Bold(true)

	navActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#3b4261"))

	navInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7aa2f7"))

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#283457"))

	normalRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c0caf5"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565f89"))

	cyanStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7dcfff"))

	boxBorder = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3b4261"))

	badgeLocal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#9ece6a")).
			Bold(true).
			Padding(0, 1)

	badgeYouTube = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#f7768e")).
			Bold(true).
			Padding(0, 1)

	badgeSpotify = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#73daca")).
			Bold(true).
			Padding(0, 1)

	badgeJioSaavn = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#7dcfff")).
			Bold(true).
			Padding(0, 1)
)

func renderBoxTop(title string, boxWidth int) string {
	innerWidth := boxWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	titleText := ""
	if title != "" {
		titleText = " " + title + " "
	}
	titleWidth := lipgloss.Width(titleText)
	if titleWidth > innerWidth-4 {
		titleText = truncate(titleText, innerWidth-4)
		titleWidth = lipgloss.Width(titleText)
	}

	dashes := innerWidth - titleWidth - 1
	if dashes < 0 {
		dashes = 0
	}

	return boxBorder.Render("┌─") + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#c0caf5")).Render(titleText) + boxBorder.Render(strings.Repeat("─", dashes)+"┐")
}

func renderBoxBottom(boxWidth int) string {
	innerWidth := boxWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
	return boxBorder.Render("└" + strings.Repeat("─", innerWidth) + "┘")
}

func renderBoxLine(content string, boxWidth int) string {
	innerWidth := boxWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	contentWidth := lipgloss.Width(content)
	if contentWidth > innerWidth {
		content = lipgloss.NewStyle().MaxWidth(innerWidth).Render(content)
		contentWidth = lipgloss.Width(content)
	}

	padding := innerWidth - contentWidth
	if padding < 0 {
		padding = 0
	}

	return boxBorder.Render("│") + content + strings.Repeat(" ", padding) + boxBorder.Render("│")
}

func (m Model) View() string {
	width := m.width
	if width < 78 {
		width = 78
	}

	var b strings.Builder

	servers, _ := m.svc.DB.GetServers()
	hasNAS := len(servers) > 0

	compact := width < 125
	var navItems []string
	if compact {
		navItems = []string{
			m.renderNavTab("1:Home", ViewHome),
			m.renderNavTab("2:Lib", ViewLibrary),
			m.renderNavTab("3:Online", ViewOnline),
		}
		if hasNAS {
			navItems = append(navItems, m.renderNavTab("4:NAS", ViewNAS))
		}
		navItems = append(navItems,
			m.renderNavTab("5:List", ViewPlaylists),
			m.renderNavTab("6:Fav", ViewFavorites),
			m.renderNavTab("7:Queue", ViewQueue),
			m.renderNavTab("8:Jobs", ViewDownloads),
			m.renderNavTab("9:Set", ViewSettings),
		)
	} else {
		navItems = []string{
			m.renderNavTab("1:Home", ViewHome),
			m.renderNavTab("2:Library", ViewLibrary),
			m.renderNavTab("3:Online", ViewOnline),
		}
		if hasNAS {
			navItems = append(navItems, m.renderNavTab("4:Home NAS", ViewNAS))
		}
		navItems = append(navItems,
			m.renderNavTab("5:Playlists", ViewPlaylists),
			m.renderNavTab("6:Favorites", ViewFavorites),
			m.renderNavTab("7:Queue", ViewQueue),
			m.renderNavTab("8:Downloads", ViewDownloads),
			m.renderNavTab("9:Settings", ViewSettings),
		)
	}

	logo := " ✦ RHYTHM ✦ "
	if width < 85 {
		logo = "RHYTHM"
	}
	navSep := boxBorder.Render("│")
	if width >= 125 {
		navSep = boxBorder.Render(" │ ")
	}
	navLine := headerStyle.Render(logo) + " " + strings.Join(navItems, navSep)
	if lipgloss.Width(navLine) > width {
		navLine = lipgloss.NewStyle().MaxWidth(width).Render(navLine)
	}
	b.WriteString(navLine + "\n")
	b.WriteString(boxBorder.Render(strings.Repeat("─", width)) + "\n")

	b.WriteString(m.renderTopHeroSection(width) + "\n")

	b.WriteString(m.renderProgressBarBox(width) + "\n")

	b.WriteString(m.renderSearchCommandBar(width) + "\n")

	b.WriteString(m.renderBottomSplitView(width))

	status := m.statusMsg
	if time.Since(m.statusTime) > 6*time.Second {
		status = "Hotkeys: [/] Search │ [Space] Pause │ [Enter] Play │ [1-9] Tabs"
	}
	b.WriteString("\n" + cyanStyle.Bold(true).Render("STATUS: ") + lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(status))

	return b.String()
}

func (m Model) renderNavTab(label string, view ViewMode) string {
	if m.currentView == view {
		return navActiveStyle.Render(label)
	}
	return navInactiveStyle.Render(label)
}

func (m Model) renderTopHeroSection(totalWidth int) string {
	cur := m.svc.Audio.CurrentTrack()
	state := m.svc.Audio.State()
	isPlaying := (state == core.StatePlaying)

	col1Width := 36
	if totalWidth < 75 {
		col1Width = 30
	}

	col2Width := totalWidth - col1Width - 3
	showLyrics := col2Width >= 16

	metaLines := m.renderMetadataCol(cur, isPlaying)

	posSec, _ := m.svc.Audio.Progress()
	lyricLines := m.renderLyricsCol(cur, posSec)

	maxLines := len(metaLines)
	if showLyrics && len(lyricLines) > maxLines {
		maxLines = len(lyricLines)
	}

	colDivider := lipgloss.NewStyle().Foreground(lipgloss.Color("#6366f1")).Render(" │ ")

	var sb strings.Builder
	for i := 0; i < maxLines; i++ {
		l1 := ""
		if i < len(metaLines) {
			l1 = metaLines[i]
		}
		if lipgloss.Width(l1) > col1Width {
			l1 = lipgloss.NewStyle().MaxWidth(col1Width).Render(l1)
		}
		w1 := lipgloss.Width(l1)
		pad1 := col1Width - w1
		if pad1 < 0 {
			pad1 = 0
		}

		if showLyrics {
			l2 := ""
			if i < len(lyricLines) {
				l2 = lyricLines[i]
			}
			if lipgloss.Width(l2) > col2Width {
				l2 = lipgloss.NewStyle().MaxWidth(col2Width).Render(l2)
			}
			w2 := lipgloss.Width(l2)
			pad2 := col2Width - w2
			if pad2 < 0 {
				pad2 = 0
			}

			sb.WriteString(l1 + strings.Repeat(" ", pad1) + colDivider +
				l2 + strings.Repeat(" ", pad2) + "\n")
		} else {
			sb.WriteString(l1 + strings.Repeat(" ", pad1) + "\n")
		}
	}

	return sb.String()
}

func (m Model) renderMetadataCol(cur *core.Track, isPlaying bool) []string {
	title := "No Track Playing"
	artist := "-"
	year := "-"
	format := "MP3"
	location := "Local/"
	size := "-"

	if cur != nil {
		title = cur.Title
		artist = cur.Artist
		if cur.Year > 0 {
			year = fmt.Sprintf("%d", cur.Year)
		}
		if cur.LocalPath != "" {
			format = strings.ToUpper(strings.TrimPrefix(filepath.Ext(cur.LocalPath), "."))
			location = filepath.Base(filepath.Dir(cur.LocalPath)) + "/"
			size = cur.DisplayDuration()
		} else {
			ref := cur.RemoteReference
			if ref == "YouTube" {
				format = "YT AAC"
				location = "YouTube/"
			} else if ref == "Spotify" {
				format = "Spot(YT)"
				location = "Spotify/"
			} else if ref == "JioSaavn" {
				format = "AAC 320k"
				location = "JioSaavn/"
			} else if ref != "" {
				format = ref
				location = ref + "/"
			} else {
				format = "Stream"
				location = "Online/"
			}
			size = cur.DisplayDuration()
		}
	}

	spec := m.generateSpectrum(isPlaying)

	kStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	cStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

	row := func(label string, valStyled string) string {
		return kStyle.Render(label) + cStyle.Render(" : ") + valStyled
	}

	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))

	titleVal := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Bold(true).Render(truncate(title, 22))
	artistVal := valStyle.Render(truncate(artist, 22))
	yearVal := valStyle.Render(year)
	sampVal := valStyle.Render("44100KHz")
	typeVal := valStyle.Render("audio only")
	fmtVal := valStyle.Render(truncate(format, 22))
	sizeVal := valStyle.Render(size)
	locVal := valStyle.Render(truncate(location, 22))

	return []string{
		row("Name     ", titleVal),
		row("Artist   ", artistVal),
		row("year     ", yearVal),
		row("sampling ", sampVal),
		row("type     ", typeVal),
		row("format   ", fmtVal),
		row("file size", sizeVal),
		row("location ", locVal),
		spec,
	}
}

func (m Model) generateSpectrum(isPlaying bool) string {
	bars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	if !isPlaying {
		return dimStyle.Render("..........._._.._._............")
	}

	var sb strings.Builder
	for i := 0; i < 28; i++ {
		val := math.Sin(float64(i)*0.4+float64(m.animFrame)*0.3)*0.5 + 0.5
		val += math.Cos(float64(i)*0.8+float64(m.animFrame)*0.2) * 0.3
		idx := int(val * float64(len(bars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(bars) {
			idx = len(bars) - 1
		}
		var barColor string
		if idx < 3 {
			barColor = "#38bdf8"
		} else if idx < 6 {
			barColor = "#818cf8"
		} else {
			barColor = "#f43f5e"
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(barColor)).Render(string(bars[idx])))
	}
	return sb.String()
}

func (m Model) renderLyricsCol(cur *core.Track, posSec float64) []string {
	if cur == nil {
		return []string{
			dimStyle.Render("No track currently playing"),
			dimStyle.Render("Select a track from Library or Online"),
			dimStyle.Render("Or press [/] to search all sources"),
			"", "", "", "", "", "", "", "",
		}
	}

	posDuration := time.Duration(posSec * float64(time.Second))

	lyricsObj := m.currentLyrics
	if lyricsObj == nil || (lyricsObj.TrackID != cur.ID && lyricsObj.Title != cur.Title) {
		if cached := lyrics.GetLyricsCached(cur); cached != nil {
			lyricsObj = cached
		}
	}

	if lyricsObj == nil {
		spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerChar := spinners[m.animFrame%len(spinners)]
		return []string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00f0ff")).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#c084fc")).Render("Artist: " + truncate(cur.Artist, 28)),
			"",
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38bdf8")).Render(spinnerChar + " Syncing lyrics from LRCLIB..."),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Render("Querying synchronized timestamps"),
			"", "", "", "", "", "",
		}
	}

	if lyricsObj.IsFallback {
		return []string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00f0ff")).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#c084fc")).Render("Artist: " + truncate(cur.Artist, 28)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#f43f5e")).Background(lipgloss.Color("#4c0519")).Bold(true).Padding(0, 1).Render("LRCLIB: NO MATCH"),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render("No online synchronized lyrics found"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Render("Drop a .lrc file in folder for offline sync"),
			"", "", "", "", "",
		}
	}

	lines := lyricsObj.Lines
	if len(lines) == 0 {
		return []string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00f0ff")).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#c084fc")).Render("Artist: " + truncate(cur.Artist, 28)),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render("No lyrics available for this track"),
			"", "", "", "", "", "", "",
		}
	}

	lastPlayedLineID := 0
	if lyricsObj.Synced {
		for id, l := range lines {
			if l.Time <= posDuration {
				lastPlayedLineID = id + 1
			}
		}
	} else {

		if len(lines) > 0 {
			lastPlayedLineID = (int(posSec/5.0) % len(lines)) + 1
		}
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00f0ff")).Render("♪ " + truncate(cur.Title, 45))

	viewportHeight := 10
	halfHeight := viewportHeight / 2
	start := 0
	if lastPlayedLineID > halfHeight {
		start = lastPlayedLineID - halfHeight
	}
	end := start + viewportHeight
	if end > len(lines) {
		end = len(lines)
		start = end - viewportHeight
		if start < 0 {
			start = 0
		}
	}

	lyricsPlayingArrow := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#f43f5e"))

	lyricsPlayingStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1e1b4b"))

	lyricsPlayedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748b"))

	lyricsNextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#93c5fd"))

	lyricsUpcomingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#cbd5e1"))

	var res []string
	res = append(res, header)

	for i := start; i < end; i++ {
		lineNum := i + 1
		lineText := lines[i].Text
		if lineText == "" {
			lineText = "♪"
		}

		if lineNum < lastPlayedLineID {

			res = append(res, lyricsPlayedStyle.Render("  "+lineText))
		} else if lineNum == lastPlayedLineID {

			res = append(res, lyricsPlayingArrow.Render("▶ ")+lyricsPlayingStyle.Render(" "+lineText+" "))
		} else if lineNum == lastPlayedLineID+1 {

			res = append(res, lyricsNextStyle.Render("  "+lineText))
		} else {

			res = append(res, lyricsUpcomingStyle.Render("  "+lineText))
		}
	}

	for len(res) < 11 {
		res = append(res, "")
	}

	return res
}

func (m Model) renderProgressBarBox(totalWidth int) string {
	boxWidth := totalWidth
	posSec, durSec := m.svc.Audio.Progress()
	posMins, posSeconds := int(posSec)/60, int(posSec)%60
	durMins, durSeconds := int(durSec)/60, int(durSec)%60

	pct := 0.0
	if durSec > 0 {
		pct = posSec / durSec
		if pct > 1.0 {
			pct = 1.0
		}
	}

	state := m.svc.Audio.State()
	playBtn := "[ PLAY ]"
	if state == core.StatePlaying {
		playBtn = "[ PAUSE ]"
	}

	waveWidth := boxWidth - 75
	if waveWidth < 12 {
		waveWidth = 12
	}

	waveBars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	filledChars := int(pct * float64(waveWidth))

	var waveStr strings.Builder
	for i := 0; i < waveWidth; i++ {
		h := int((math.Sin(float64(i)*0.25)*0.5 + 0.5) * float64(len(waveBars)-1))
		r := waveBars[h]
		if i < filledChars {
			waveStr.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff")).Render(string(r)))
		} else {
			waveStr.WriteString(dimStyle.Render(string(r)))
		}
	}

	vol := m.svc.Audio.Volume()
	volFilled := int((float64(vol) / 100.0) * 10)
	if volFilled > 10 {
		volFilled = 10
	}
	volBar := strings.Repeat("█", volFilled) + strings.Repeat("░", 10-volFilled)

	top := renderBoxTop("PROGRESS BAR", boxWidth)

	timeBadge := fmt.Sprintf("[%02d:%02d/%02d:%02d]", posMins, posSeconds, durMins, durSeconds)
	controls := fmt.Sprintf("[ ◀◀ ] %s [ ▶▶ ]  VOLUME BAR: [%s] %d%%", playBtn, volBar, vol)

	lineContent := fmt.Sprintf(" %s  %s  %s", timeBadge, waveStr.String(), controls)
	mid := renderBoxLine(lineContent, boxWidth)
	bot := renderBoxBottom(boxWidth)

	return top + "\n" + mid + "\n" + bot
}

func (m Model) renderSearchCommandBar(totalWidth int) string {
	boxWidth := totalWidth
	prompt := "/: "

	var content string
	var rightHint string

	if m.searching {
		content = m.searchBuffer + "█"
		rightHint = "[Enter] Search Unified │ [Esc] Cancel"
	} else if m.unifiedQuery != "" {
		content = fmt.Sprintf("Search: '%s' (%d total)", m.unifiedQuery, len(m.unifiedAll))
		rightHint = "[/] Edit │ [Tab] Filter Source │ [Esc] Clear"
	} else {
		content = "Press '/' or ':' to search all sources, or type /setting, /library, /nas..."
		rightHint = "[/] Search All │ [1-9] Tabs"
	}

	top := renderBoxTop("SEARCH / COMMAND", boxWidth)

	innerWidth := boxWidth - 2
	leftStr := prompt + content
	if m.searching {
		leftStr = prompt + cyanStyle.Render(content)
	} else if m.unifiedQuery != "" {
		leftStr = prompt + lipgloss.NewStyle().Foreground(lipgloss.Color("#EEEEEE")).Bold(true).Render(content)
	} else {
		leftStr = prompt + dimStyle.Render(content)
	}

	space := innerWidth - lipgloss.Width(leftStr) - lipgloss.Width(rightHint) - 2
	var lineContent string
	if space > 0 {
		lineContent = " " + leftStr + strings.Repeat(" ", space) + dimStyle.Render(rightHint) + " "
	} else {
		lineContent = " " + leftStr
	}

	mid := renderBoxLine(lineContent, boxWidth)
	bot := renderBoxBottom(boxWidth)

	return top + "\n" + mid + "\n" + bot
}

func (m Model) renderBottomSplitView(totalWidth int) string {
	leftWidth := (totalWidth - 2) * 60 / 100
	if leftWidth < 46 {
		leftWidth = 46
	}
	rightWidth := totalWidth - leftWidth - 2
	if rightWidth < 28 {
		rightWidth = 28
		leftWidth = totalWidth - rightWidth - 2
	}

	heroH := 11

	targetRows := m.height - (heroH + 13)
	if targetRows < 4 {
		targetRows = 4
	}
	if targetRows > 22 {
		targetRows = 22
	}

	leftBox := m.renderLeftListPanel(leftWidth, targetRows)
	rightBox := m.renderRightQueuePanel(rightWidth, targetRows)

	leftLines := strings.Split(leftBox, "\n")
	rightLines := strings.Split(rightBox, "\n")

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	var sb strings.Builder
	for i := 0; i < maxLines; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		} else {
			l = strings.Repeat(" ", leftWidth)
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		} else {
			r = strings.Repeat(" ", rightWidth)
		}
		sb.WriteString(l + "  " + r + "\n")
	}

	return sb.String()
}

func (m Model) renderLeftListPanel(width int, targetRows int) string {
	var title string
	var tracks []core.Track
	isUnified := false

	if m.unifiedQuery != "" && len(m.unifiedAll) > 0 && (m.currentView == ViewHome || m.currentView == ViewOnline || m.currentView == ViewLibrary) {
		isUnified = true
		title = fmt.Sprintf("UNIFIED SEARCH: '%s'", truncate(m.unifiedQuery, 16))
		tracks = m.unifiedTracks
	} else {
		switch m.currentView {
		case ViewOnline:
			title = "ONLINE STREAM SEARCH"
			tracks = m.onlineTracks
		case ViewNAS:
			title = "NAS REMOTE LIBRARY"
			tracks = m.nasTracks
		case ViewFavorites:
			title = "FAVORITE SONGS"
			tracks, _ = m.svc.DB.GetFavorites()
		case ViewSettings:
			return m.renderSettingsBox(width, targetRows)
		case ViewDownloads:
			return m.renderDownloadsBox(width, targetRows)
		case ViewPlaylists:
			title = "PLAYLISTS"
			lists, _ := m.svc.DB.GetPlaylists()
			for _, l := range lists {
				tracks = append(tracks, core.Track{Title: l.Name, Artist: fmt.Sprintf("%d tracks", len(l.Tracks))})
			}
		default:
			title = "LOCAL AUDIO FILES (sort: folder order)"
			tracks = m.libraryTracks
		}
	}

	var lines []string
	lines = append(lines, renderBoxTop(title, width))

	if isUnified {
		pillAll := fmt.Sprintf("[1:All %d]", len(m.unifiedAll))
		pillLocal := fmt.Sprintf("[2:Local %d]", len(m.unifiedLocal))
		var ytCount, spCount, jsCount int
		for _, t := range m.unifiedOnline {
			if t.RemoteReference == "YouTube" {
				ytCount++
			} else if t.RemoteReference == "Spotify" {
				spCount++
			} else if t.RemoteReference == "JioSaavn" {
				jsCount++
			}
		}
		pillYT := fmt.Sprintf("[3:YT %d]", ytCount)
		pillSP := fmt.Sprintf("[4:SP %d]", spCount)
		pillJS := fmt.Sprintf("[5:JS %d]", jsCount)

		filterPills := []string{pillAll, pillLocal, pillYT, pillSP, pillJS}
		for idx, p := range filterPills {
			if idx == m.searchFilter {
				filterPills[idx] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#3b4261")).Render(p)
			} else {
				filterPills[idx] = dimStyle.Render(p)
			}
		}
		filterBar := " " + strings.Join(filterPills, " ")
		lines = append(lines, renderBoxLine(filterBar, width))
	}

	if len(tracks) == 0 {
		if m.isSearchingUnified {
			lines = append(lines, renderBoxLine(" ⟳ Searching across Local, YouTube, Spotify & JioSaavn...", width))
		} else if isUnified {
			lines = append(lines, renderBoxLine(" No results for this filter. Press [Tab] to switch sources.", width))
		} else {
			lines = append(lines, renderBoxLine(" No tracks available. Press [/] to search online or add local files.", width))
		}
		for len(lines) < targetRows+2 {
			lines = append(lines, renderBoxLine("", width))
		}
		lines = append(lines, renderBoxBottom(width))
		return strings.Join(lines, "\n")
	}

	visibleRows := targetRows
	if isUnified {
		visibleRows--
	}
	if visibleRows < 4 {
		visibleRows = 4
	}

	start := 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	end := start + visibleRows
	if end > len(tracks) {
		end = len(tracks)
	}

	for i := start; i < end; i++ {
		t := tracks[i]
		dur := t.DisplayDuration()
		artist := truncate(t.Artist, 14)
		badge := m.renderSourceBadge(t)

		fixedWidth := 6 + lipgloss.Width(badge) + 2 + 14 + 3 + len(dur) + 2
		titleWidth := width - fixedWidth - 2
		if titleWidth < 10 {
			titleWidth = 10
		}
		songTitle := truncate(t.Title, titleWidth)

		row := fmt.Sprintf(" %2d │ %s %-*s │ %-14s │ %s",
			i+1, badge, titleWidth, songTitle, artist, dur)

		if i == m.cursor {
			highlighted := selectedRowStyle.Render(fmt.Sprintf("▶%s", row[1:]))
			lines = append(lines, renderBoxLine(highlighted, width))
		} else {
			lines = append(lines, renderBoxLine(normalRowStyle.Render(row), width))
		}
	}

	for len(lines) < targetRows+2 {
		lines = append(lines, renderBoxLine("", width))
	}
	lines = append(lines, renderBoxBottom(width))

	return strings.Join(lines, "\n")
}

func (m Model) renderSourceBadge(t core.Track) string {
	ref := t.RemoteReference
	if t.Source == core.SourceLocal || ref == "Local" || t.LocalPath != "" {
		return badgeLocal.Render("LOCAL")
	}
	switch ref {
	case "YouTube":
		return badgeYouTube.Render("YT")
	case "Spotify":
		return badgeSpotify.Render("SPOTIFY")
	case "JioSaavn":
		return badgeJioSaavn.Render("JIOSAAVN")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#e0af68")).Bold(true).Padding(0, 1).Render("ONLINE")
	}
}

func (m Model) renderRightQueuePanel(width int, targetRows int) string {
	qLen := m.svc.Queue.Len()
	title := fmt.Sprintf("QUEUE (%d)", qLen)

	var lines []string
	lines = append(lines, renderBoxTop(title, width))

	items := m.svc.Queue.Items()
	if len(items) == 0 {
		centerPadding := targetRows / 2
		for i := 0; i < centerPadding-1; i++ {
			lines = append(lines, renderBoxLine("", width))
		}
		lines = append(lines, renderBoxLine(centerText("ADD TRACKS TO QUEUE (Press 'a')", width-2), width))
		for len(lines) < targetRows+2 {
			lines = append(lines, renderBoxLine("", width))
		}
	} else {
		curIdx := m.svc.Queue.CurrentIndex()
		for i := 0; i < targetRows; i++ {
			if i >= len(items) {
				lines = append(lines, renderBoxLine("", width))
				continue
			}
			it := items[i]
			marker := "  "
			if i == curIdx {
				marker = "▶ "
			}

			avail := width - 10
			if avail < 8 {
				avail = 8
			}
			row := fmt.Sprintf("%s%2d │ %s", marker, i+1, truncate(it.Track.Title, avail))
			if i == curIdx {
				lines = append(lines, renderBoxLine(selectedRowStyle.Render(row), width))
			} else {
				lines = append(lines, renderBoxLine(dimStyle.Render(row), width))
			}
		}
	}

	lines = append(lines, renderBoxBottom(width))
	return strings.Join(lines, "\n")
}

func (m Model) renderSettingsBox(width int, targetRows int) string {
	var lines []string
	lines = append(lines, renderBoxTop("SETTINGS & SERVERS", width))

	servers, _ := m.svc.DB.GetServers()
	lines = append(lines, renderBoxLine(" [0] + Add Remote Server / NAS (Reachability, auth, streaming)", width))
	if len(servers) == 0 {
		lines = append(lines, renderBoxLine("   (No servers configured. Add one to unlock remote NAS)", width))
	} else {
		for i, s := range servers {
			row := fmt.Sprintf("   [%d] %s -> %s (User: %s)", i+1, s.Name, s.BaseURL(), s.Username)
			lines = append(lines, renderBoxLine(row, width))
		}
	}
	lines = append(lines, renderBoxLine(fmt.Sprintf(" Audio Volume: %d%% │ Cache: %s", m.svc.Config.Audio.Volume, m.svc.Cache.FormattedStats()), width))

	for len(lines) < targetRows+2 {
		lines = append(lines, renderBoxLine("", width))
	}
	lines = append(lines, renderBoxBottom(width))
	return strings.Join(lines, "\n")
}

func (m Model) renderDownloadsBox(width int, targetRows int) string {
	var lines []string
	lines = append(lines, renderBoxTop("NAS ACQUISITION JOBS", width))

	if len(m.remoteJobs) == 0 {
		lines = append(lines, renderBoxLine(" No remote download jobs. Press [N] on any track to save to NAS.", width))
	} else {
		for i, j := range m.remoteJobs {
			if i >= targetRows {
				break
			}
			row := fmt.Sprintf(" %2d │ %s - %s [%s: %.0f%%]", i+1, j.TrackArtist, j.TrackTitle, j.Status, j.Progress)
			lines = append(lines, renderBoxLine(row, width))
		}
	}

	for len(lines) < targetRows+2 {
		lines = append(lines, renderBoxLine("", width))
	}
	lines = append(lines, renderBoxBottom(width))
	return strings.Join(lines, "\n")
}

func centerText(s string, width int) string {
	l := lipgloss.Width(s)
	if l >= width {
		return s
	}
	pad := (width - l) / 2
	return strings.Repeat(" ", pad) + s
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) > max {
		if max > 3 {
			return s[:max-3] + "..."
		}
		return s[:max]
	}
	return s
}
