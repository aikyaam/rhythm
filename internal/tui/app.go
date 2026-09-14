package tui

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rhythm/internal/cli"
	"rhythm/internal/config"
	"rhythm/internal/core"
	"rhythm/internal/graphics"
	"rhythm/internal/lyrics"
	"rhythm/internal/metadata"
	"rhythm/internal/providers"
	"rhythm/internal/theme"

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

type PopupMode int

const (
	PopupNone PopupMode = iota
	PopupActions
	PopupTheme
	PopupHelp
	PopupPlaylistSelect
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

	currentTheme     theme.Theme
	popup            PopupMode
	popupCursor      int
	popupActionTrack *core.Track
	lyricOffset      time.Duration
	prevVolume       int
	showThumbnail    bool
	imageProtocol    metadata.ImageProtocol
	thumbnailTrackID string
	thumbnailLines   []string
	lastKey          string
	lastKeyEvent     time.Time

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

type thumbnailLoadedMsg struct {
	trackID string
	lines   []string
}

func loadThumbnailCmd(track *core.Track, w, h int, proto metadata.ImageProtocol) tea.Cmd {
	if track == nil {
		return nil
	}
	tr := *track
	return func() tea.Msg {
		lines := metadata.GetTrackCoverThumbnailProto(&tr, w, h, proto)
		return thumbnailLoadedMsg{
			trackID: tr.ID,
			lines:   lines,
		}
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

var (
	currentThemeTheme   theme.Theme
	headerStyle         lipgloss.Style
	navActiveStyle      lipgloss.Style
	navInactiveStyle    lipgloss.Style
	selectedRowStyle    lipgloss.Style
	normalRowStyle      lipgloss.Style
	dimStyle            lipgloss.Style
	cyanStyle           lipgloss.Style
	boxBorder           lipgloss.Style
	lyricsPlayingStyle  lipgloss.Style
	lyricsPlayingArrow  lipgloss.Style
	lyricsPlayedStyle   lipgloss.Style
	lyricsNextStyle     lipgloss.Style
	lyricsUpcomingStyle lipgloss.Style
	badgeLocal          lipgloss.Style
	badgeYouTube        lipgloss.Style
	badgeSpotify        lipgloss.Style
	badgeJioSaavn       lipgloss.Style
)

func applyTheme(t theme.Theme) {
	currentThemeTheme = t
	pal := theme.NewPalette(t)
	headerStyle = pal.Header
	navActiveStyle = pal.NavActive
	navInactiveStyle = pal.NavInactive
	selectedRowStyle = pal.SelectedRow
	normalRowStyle = pal.NormalRow
	dimStyle = pal.Dim
	cyanStyle = pal.Accent
	boxBorder = pal.Border
	lyricsPlayingStyle = pal.LyricPlaying
	lyricsPlayingArrow = pal.PlayingArrow
	lyricsPlayedStyle = pal.LyricPlayed
	lyricsNextStyle = pal.LyricNext
	lyricsUpcomingStyle = pal.LyricUp

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
}

func InitialModel(svc *cli.AppServices) Model {
	tracks, _ := svc.Library.AllTracks()
	thID := "tokyo-night"
	if svc.Config != nil && svc.Config.Theme != "" {
		thID = svc.Config.Theme
	}
	th := theme.GetTheme(thID)
	applyTheme(th)

	imgProto := metadata.ProtocolSixel
	if svc.Config != nil && svc.Config.Image.Protocol != "" && svc.Config.Image.Protocol != "auto" {
		imgProto = metadata.ImageProtocol(svc.Config.Image.Protocol)
	}

	m := Model{
		svc:           svc,
		currentView:   ViewHome,
		currentTheme:  th,
		cursor:        0,
		width:         110,
		height:        35,
		libraryTracks: tracks,
		showThumbnail: true,
		imageProtocol: imgProto,
		statusMsg:     "Ready. Press [?] for shortcuts, [/] to search, [T] for themes",
		statusTime:    time.Now(),
		prevVolume:    80,
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.EnableMouseCellMotion)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			if m.popup != PopupNone {
				if m.popupCursor > 0 {
					m.popupCursor--
				}
			} else if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelDown {
			if m.popup != PopupNone {
				m.popupCursor++
			} else if m.cursor < m.currentListLength()-1 {
				m.cursor++
			}
			return m, nil
		}
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if m.popup != PopupNone {
				return m, nil
			}
			if msg.Y >= 13 && msg.Y <= 15 && m.width > 0 {
				_, durSec := m.svc.Audio.Progress()
				if durSec > 0 {
					ratio := float64(msg.X) / float64(m.width)
					if ratio < 0 {
						ratio = 0
					}
					if ratio > 1 {
						ratio = 1
					}
					targetSec := ratio * durSec
					pos, _ := m.svc.Audio.Progress()
					m.svc.Audio.Seek(targetSec - pos)
					m.setStatus(fmt.Sprintf("Seeked to %02d:%02d", int(targetSec)/60, int(targetSec)%60))
				}
				return m, nil
			}
			if msg.Y >= 19 && msg.Y < 19+m.height {
				rowIdx := msg.Y - 19
				if rowIdx >= 0 && rowIdx < m.currentListLength() {
					m.cursor = rowIdx
					return m, m.handleSelection()
				}
			}
		}

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
			if cur.ID != m.thumbnailTrackID {
				m.thumbnailTrackID = cur.ID
				if cached := metadata.GetCachedThumbnailProto(cur, 22, 11, m.imageProtocol); len(cached) > 0 {
					m.thumbnailLines = cached
				} else {
					m.thumbnailLines = metadata.GenerateFallbackArtwork(22, 11, cur.Title, cur.Artist)
					cmds = append(cmds, loadThumbnailCmd(cur, 22, 11, m.imageProtocol))
				}
			}
		} else if m.thumbnailTrackID != "" {
			m.thumbnailTrackID = ""
			m.thumbnailLines = nil
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

	case thumbnailLoadedMsg:
		if msg.trackID == m.thumbnailTrackID && len(msg.lines) > 0 {
			m.thumbnailLines = msg.lines
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
				m.setStatus(fmt.Sprintf("Found %d results (%d Local, %d Online)! [Enter] Play, [Tab] Filter, [a] Actions",
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
			if msg.track.ID != m.thumbnailTrackID {
				m.thumbnailTrackID = msg.track.ID
				if cached := metadata.GetCachedThumbnailProto(&msg.track, 22, 11, m.imageProtocol); len(cached) > 0 {
					m.thumbnailLines = cached
				} else {
					m.thumbnailLines = metadata.GenerateFallbackArtwork(22, 11, msg.track.Title, msg.track.Artist)
					cmds = append(cmds, loadThumbnailCmd(&msg.track, 22, 11, m.imageProtocol))
				}
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

		if m.popup != PopupNone {
			switch key {
			case "ctrl+c":
				graphics.GetCoverWindow().Close()
				return m, tea.Quit
			case "esc", "q":
				m.popup = PopupNone
				return m, nil
			case "up", "k":
				if m.popupCursor > 0 {
					m.popupCursor--
				}
				return m, nil
			case "down", "j":
				m.popupCursor++
				return m, nil
			case "enter":
				return m.handlePopupEnter()
			}
			return m, nil
		}

		now := time.Now()
		if m.lastKey != "" && now.Sub(m.lastKeyEvent) < 1000*time.Millisecond {
			prevKey := m.lastKey
			m.lastKey = ""
			switch prevKey {
			case "g":
				if key == "g" {
					m.cursor = 0
					return m, nil
				} else if key == "a" {
					m.openActionPopup(m.getSelectedTrack())
					return m, nil
				} else if key == "L" || key == "l" {
					m.setStatus("Focused Live Synced Lyrics view")
					return m, nil
				}
			case "s", "S":
				switch key {
				case "t":
					if prevKey == "s" {
						m.svc.Queue.ToggleShuffle()
					}
					m.sortCurrentTracks("title")
					return m, nil
				case "a":
					if prevKey == "s" {
						m.svc.Queue.ToggleShuffle()
					}
					m.sortCurrentTracks("artist")
					return m, nil
				case "d":
					if prevKey == "s" {
						m.svc.Queue.ToggleShuffle()
					}
					m.sortCurrentTracks("duration")
					return m, nil
				case "r":
					if prevKey == "s" {
						m.svc.Queue.ToggleShuffle()
					}
					m.sortCurrentTracks("reverse")
					return m, nil
				}
			}
		}

		switch key {
		case "ctrl+c":
			graphics.GetCoverWindow().Close()
			return m, tea.Quit

		case "g":
			m.lastKey = key
			m.lastKeyEvent = time.Now()
			return m, nil

		case "w":
			cur := m.svc.Audio.CurrentTrack()
			if cur != nil {
				artPath := metadata.GetTrackArtworkPathOrURL(cur)
				_ = graphics.GetCoverWindow().UpdateCover(artPath, cur.Title, cur.Artist)
			}
			visible := graphics.GetCoverWindow().Toggle()
			if visible {
				m.setStatus("Cover Window: OPENED")
			} else {
				m.setStatus("Cover Window: CLOSED")
			}
			return m, nil

		case "?", "ctrl+h":
			m.popup = PopupHelp
			m.popupCursor = 0
			return m, nil

		case "t", "T":
			m.popup = PopupTheme
			m.popupCursor = 0
			return m, nil

		case "o", "ctrl+@", "ctrl+space", "ctrl+ ":
			m.openActionPopup(m.getSelectedTrack())
			return m, nil

		case "a":
			cur := m.svc.Audio.CurrentTrack()
			if cur != nil {
				m.openActionPopup(cur)
			} else {
				m.openActionPopup(m.getSelectedTrack())
			}
			return m, nil

		case "Z", "ctrl+z":
			if t := m.getSelectedTrack(); t != nil {
				m.svc.Queue.Add(*t)
				m.setStatus(fmt.Sprintf("Added to queue: %s", t.Title))
			}
			return m, nil

		case "z":
			m.currentView = ViewQueue
			m.cursor = 0
			return m, nil

		case "l":
			m.setStatus("Focused Live Synced Lyrics view")
			return m, nil

		case "v", "V":
			if !m.showThumbnail {
				m.showThumbnail = true
				m.imageProtocol = metadata.ProtocolHalfblocks
			} else {
				switch m.imageProtocol {
				case metadata.ProtocolHalfblocks:
					m.imageProtocol = metadata.ProtocolBraille
				case metadata.ProtocolBraille:
					m.imageProtocol = metadata.ProtocolSixel
				case metadata.ProtocolSixel:
					m.imageProtocol = metadata.ProtocolKitty
				case metadata.ProtocolKitty:
					m.imageProtocol = metadata.ProtocolITerm2
				default:
					m.showThumbnail = false
				}
			}

			if !m.showThumbnail {
				m.setStatus("Album Cover Thumbnail: HIDDEN")
				return m, nil
			}

			cur := m.svc.Audio.CurrentTrack()
			if cur != nil {
				m.thumbnailLines = metadata.GetTrackCoverThumbnailProto(cur, 22, 11, m.imageProtocol)
			}
			m.setStatus(fmt.Sprintf("Image Mode: %s", metadata.ProtocolDisplayName(m.imageProtocol)))
			if cur != nil {
				return m, loadThumbnailCmd(cur, 22, 11, m.imageProtocol)
			}
			return m, nil

		case "q", "esc":
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
			graphics.GetCoverWindow().Close()
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

		case " ", "space":
			m.svc.Audio.TogglePlayPause()
		case "n":
			if next := m.svc.Queue.Next(); next != nil {
				_ = m.svc.Audio.Play(next)
				_ = m.svc.History.Record(*next, 0)
			}
		case "p":
			if prev := m.svc.Queue.Previous(); prev != nil {
				_ = m.svc.Audio.Play(prev)
			}

		case ".":
			m.playRandomTrack()
			return m, nil

		case "_":
			vol := m.svc.Audio.Volume()
			if vol > 0 {
				m.prevVolume = vol
				m.svc.Audio.SetVolume(0)
				m.setStatus("🔇 Muted (press _ to unmute)")
			} else {
				target := m.prevVolume
				if target <= 0 {
					target = 80
				}
				m.svc.Audio.SetVolume(target)
				m.setStatus(fmt.Sprintf("🔊 Unmuted: %d%%", target))
			}

		case "^":
			pos, _ := m.svc.Audio.Progress()
			m.svc.Audio.Seek(-pos)
			m.setStatus("Seeked to start (00:00)")

		case "+", "=":
			v := m.svc.Audio.AdjustVolume(5)
			m.setStatus(fmt.Sprintf("Volume: %d%%", v))
		case "-":
			v := m.svc.Audio.AdjustVolume(-5)
			m.setStatus(fmt.Sprintf("Volume: %d%%", v))
		case "right", ">":
			m.svc.Audio.Seek(10)
		case "left", "<":
			m.svc.Audio.Seek(-10)

		case "[":
			m.lyricOffset -= 250 * time.Millisecond
			m.setStatus(fmt.Sprintf("Lyric Sync Offset: %v", m.lyricOffset))
		case "]":
			m.lyricOffset += 250 * time.Millisecond
			m.setStatus(fmt.Sprintf("Lyric Sync Offset: %v", m.lyricOffset))

		case "up", "k", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j", "ctrl+n":
			maxLen := m.currentListLength()
			if m.cursor < maxLen-1 {
				m.cursor++
			}
		case "G", "end":
			maxLen := m.currentListLength()
			if maxLen > 0 {
				m.cursor = maxLen - 1
			}
		case "home":
			m.cursor = 0
		case "ctrl+f", "pgdown":
			m.cursor += 10
			maxLen := m.currentListLength()
			if m.cursor >= maxLen && maxLen > 0 {
				m.cursor = maxLen - 1
			}
		case "ctrl+b", "pgup":
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}

		case "enter":
			return m, m.handleSelection()

		case "f":
			t := m.getSelectedTrack()
			if t == nil {
				t = m.svc.Audio.CurrentTrack()
			}
			if t != nil {
				if m.svc.DB.IsFavorite(t.ID) {
					_ = m.svc.DB.RemoveFavorite(t.ID)
					m.setStatus(fmt.Sprintf("Removed from favorites: %s", t.Title))
				} else {
					_ = m.svc.DB.AddFavorite(t)
					m.setStatus(fmt.Sprintf("Added to favorites: %s", t.Title))
				}
			} else {
				m.setStatus("No track selected or playing to favorite.")
			}

		case "N":
			m.handleSaveToNAS()

		case "/", ":":
			m.searching = true
			m.searchBuffer = ""

		case "s", "ctrl+s":
			shuf := m.svc.Queue.ToggleShuffle()
			if shuf {
				m.setStatus("Shuffle: ON")
			} else {
				m.setStatus("Shuffle: OFF")
			}
			m.lastKey = "s"
			m.lastKeyEvent = time.Now()
			return m, nil

		case "S":
			m.sortCurrentTracks("title")
			m.lastKey = "S"
			m.lastKeyEvent = time.Now()
			return m, nil

		case "r", "ctrl+r":
			mode := m.svc.Queue.CycleRepeat()
			m.setStatus(fmt.Sprintf("Repeat: %s", mode))

		case "d", "x":
			if m.currentView == ViewQueue && m.svc.Queue.Len() > 0 {
				m.svc.Queue.Remove(m.cursor)
				if m.cursor >= m.svc.Queue.Len() && m.cursor > 0 {
					m.cursor--
				}
				m.setStatus("Removed track from queue")
			}
		case "K", "ctrl+k":
			if m.currentView == ViewQueue && m.cursor > 0 {
				m.svc.Queue.Move(m.cursor, m.cursor-1)
				m.cursor--
			}
		case "J", "ctrl+j":
			if m.currentView == ViewQueue && m.cursor < m.svc.Queue.Len()-1 {
				m.svc.Queue.Move(m.cursor, m.cursor+1)
				m.cursor++
			}
		case "C":
			if m.currentView == ViewQueue {
				m.svc.Queue.Clear()
				m.cursor = 0
				m.setStatus("Queue cleared")
			}
		}
	}

	return m, nil
}

func (m *Model) openActionPopup(t *core.Track) {
	if t == nil {
		m.setStatus("No track selected for actions.")
		return
	}
	m.popup = PopupActions
	m.popupActionTrack = t
	m.popupCursor = 0
}

func (m *Model) playRandomTrack() {
	tracks := m.currentTrackList()
	if len(tracks) == 0 {
		m.setStatus("No tracks available to shuffle.")
		return
	}
	idx := rand.Intn(len(tracks))
	m.cursor = idx
	t := tracks[idx]
	_ = m.svc.Audio.Play(&t)
	_ = m.svc.History.Record(t, 0)
	m.setStatus(fmt.Sprintf("🔀 Shuffled to: %s - %s", t.Artist, t.Title))
}

func (m *Model) currentTrackList() []core.Track {
	if m.unifiedQuery != "" && len(m.unifiedTracks) > 0 {
		return m.unifiedTracks
	}
	switch m.currentView {
	case ViewLibrary:
		return m.libraryTracks
	case ViewOnline:
		return m.onlineTracks
	case ViewNAS:
		return m.nasTracks
	case ViewFavorites:
		favs, _ := m.svc.DB.GetFavorites()
		return favs
	default:
		return m.libraryTracks
	}
}

func (m *Model) sortCurrentTracks(by string) {
	tracks := m.currentTrackList()
	if len(tracks) == 0 {
		return
	}
	switch by {
	case "title":
		sort.Slice(tracks, func(i, j int) bool {
			return strings.ToLower(tracks[i].Title) < strings.ToLower(tracks[j].Title)
		})
		m.setStatus("Sorted by Title (A-Z)")
	case "artist":
		sort.Slice(tracks, func(i, j int) bool {
			return strings.ToLower(tracks[i].Artist) < strings.ToLower(tracks[j].Artist)
		})
		m.setStatus("Sorted by Artist (A-Z)")
	case "duration":
		sort.Slice(tracks, func(i, j int) bool {
			return tracks[i].Duration < tracks[j].Duration
		})
		m.setStatus("Sorted by Duration")
	case "reverse":
		for i, j := 0, len(tracks)-1; i < j; i, j = i+1, j-1 {
			tracks[i], tracks[j] = tracks[j], tracks[i]
		}
		m.setStatus("Reversed list order")
	}

	if m.unifiedQuery != "" {
		m.unifiedTracks = tracks
	} else {
		switch m.currentView {
		case ViewLibrary:
			m.libraryTracks = tracks
		case ViewOnline:
			m.onlineTracks = tracks
		case ViewNAS:
			m.nasTracks = tracks
		}
	}
	m.cursor = 0
}

func (m *Model) handlePopupEnter() (tea.Model, tea.Cmd) {
	switch m.popup {
	case PopupTheme:
		if m.popupCursor >= 0 && m.popupCursor < len(theme.Presets) {
			sel := theme.Presets[m.popupCursor]
			m.currentTheme = sel
			applyTheme(sel)
			if m.svc.Config != nil {
				m.svc.Config.Theme = sel.ID
				_ = config.Save(m.svc.Config)
			}
			m.setStatus(fmt.Sprintf("Switched theme to: %s", sel.Name))
		}
		m.popup = PopupNone
		return m, nil

	case PopupActions:
		if m.popupActionTrack == nil {
			m.popup = PopupNone
			return m, nil
		}
		track := *m.popupActionTrack
		actionIdx := m.popupCursor
		m.popup = PopupNone

		switch actionIdx {
		case 0:
			m.setStatus(fmt.Sprintf("▶ Playing: %s", track.Title))
			return m, func() tea.Msg {
				_ = m.svc.Audio.Play(&track)
				_ = m.svc.History.Record(track, 0)
				return streamResolvedMsg{track: track}
			}
		case 1:
			m.svc.Queue.Add(track)
			m.setStatus(fmt.Sprintf("Added to queue: %s", track.Title))
		case 2:
			if m.svc.DB.IsFavorite(track.ID) {
				_ = m.svc.DB.RemoveFavorite(track.ID)
				m.setStatus(fmt.Sprintf("Removed from favorites: %s", track.Title))
			} else {
				_ = m.svc.DB.AddFavorite(&track)
				m.setStatus(fmt.Sprintf("Added to favorites: %s", track.Title))
			}
		case 3:
			m.popup = PopupPlaylistSelect
			m.popupCursor = 0
			return m, nil
		case 4:
			servers, _ := m.svc.DB.GetServers()
			if len(servers) > 0 {
				go func(s core.ServerConfig, t core.Track) {
					_, _ = m.svc.Remote.SaveToNAS(&s, &t)
				}(servers[0], track)
				m.setStatus(fmt.Sprintf("Sent save request to NAS: %s", track.Title))
			} else {
				m.setStatus("⚠️ No NAS configured. Add one under Settings.")
			}
		case 5:
			m.unifiedQuery = track.Artist
			m.isSearchingUnified = true
			m.searchFilter = 0
			m.setStatus(fmt.Sprintf("Searching artist: %s", track.Artist))
			return m, unifiedSearchCmd(m.svc, track.Artist)
		case 6:
			url := track.StreamURL
			if url == "" {
				url = track.LocalPath
			}
			m.setStatus(fmt.Sprintf("Track Link: %s", truncate(url, 40)))
		}
		return m, nil

	case PopupPlaylistSelect:
		lists := m.svc.Playlists.List()
		if m.popupCursor >= 0 && m.popupCursor < len(lists) && m.popupActionTrack != nil {
			targetList := lists[m.popupCursor]
			_ = m.svc.Playlists.AddTrack(targetList.ID, *m.popupActionTrack)
			m.setStatus(fmt.Sprintf("Added '%s' to playlist '%s'", m.popupActionTrack.Title, targetList.Name))
		}
		m.popup = PopupNone
		return m, nil

	default:
		m.popup = PopupNone
		return m, nil
	}
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
	case ViewSettings:
		servers, _ := m.svc.DB.GetServers()
		return 1 + len(servers)
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
	case ViewQueue:
		items := m.svc.Queue.Items()
		if m.cursor >= 0 && m.cursor < len(items) {
			t := items[m.cursor].Track
			return &t
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

	if m.currentView == ViewPlaylists {
		lists, _ := m.svc.DB.GetPlaylists()
		if m.cursor >= 0 && m.cursor < len(lists) {
			pl := lists[m.cursor]
			if len(pl.Tracks) > 0 {
				m.svc.Queue.Clear()
				for _, tr := range pl.Tracks {
					m.svc.Queue.Add(tr)
				}
				first := pl.Tracks[0]
				m.setStatus(fmt.Sprintf("Playing playlist '%s' (%d tracks)", pl.Name, len(pl.Tracks)))
				_ = m.svc.Audio.Play(&first)
				_ = m.svc.History.Record(first, 0)
			} else {
				m.setStatus(fmt.Sprintf("Playlist '%s' is empty", pl.Name))
			}
		}
		return nil
	}

	if m.currentView == ViewQueue {
		m.svc.Queue.JumpTo(m.cursor)
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
			m.setStatus(fmt.Sprintf("Save to NAS failed: %v", err))
			return
		}
		m.setStatus(fmt.Sprintf("Remote Job Queued on %s! NAS is acquiring '%s'", s.Name, t.Title))
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
		case "theme", "themes":
			m.popup = PopupTheme
			m.popupCursor = 0
			m.searchBuffer = ""
			return m, nil
		case "help":
			m.popup = PopupHelp
			m.popupCursor = 0
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
					m.setStatus(fmt.Sprintf("Server %s unreachable: %s", s.Name, test.ErrorMessage))
					return
				}
				_ = m.svc.DB.SaveServer(&s)
				m.setStatus(fmt.Sprintf("Server '%s' connected & verified! NAS menu is now unlocked.", s.Name))
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

	return boxBorder.Render("┌─") + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Fg)).Render(titleText) + boxBorder.Render(strings.Repeat("─", dashes)+"┐")
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
		status = "Hotkeys: [?] Help │ [o] Actions │ [/] Search │ [T] Theme │ [Space] Pause │ [Enter] Play"
	}
	b.WriteString("\n" + cyanStyle.Bold(true).Render("STATUS: ") + lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Secondary)).Render(status))

	baseView := b.String()
	if m.popup != PopupNone {
		return m.renderPopupOverlay(baseView, width, m.height)
	}
	return baseView
}

func (m Model) renderPopupOverlay(baseView string, totalWidth, totalHeight int) string {
	boxWidth := 56
	if totalWidth < 62 {
		boxWidth = totalWidth - 4
	}

	var popContent string
	switch m.popup {
	case PopupTheme:
		popContent = m.renderThemePopup(boxWidth)
	case PopupActions:
		popContent = m.renderActionsPopup(boxWidth)
	case PopupHelp:
		popContent = m.renderHelpPopup(boxWidth + 6)
		boxWidth += 6
	case PopupPlaylistSelect:
		popContent = m.renderPlaylistSelectPopup(boxWidth)
	default:
		return baseView
	}

	baseLines := strings.Split(baseView, "\n")
	popLines := strings.Split(popContent, "\n")

	startY := (totalHeight - len(popLines)) / 2
	if startY < 2 {
		startY = 2
	}

	out := make([]string, len(baseLines))
	copy(out, baseLines)

	leftPad := (totalWidth - boxWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	for idx, pLine := range popLines {
		targetY := startY + idx
		if targetY >= 0 && targetY < len(out) {
			out[targetY] = strings.Repeat(" ", leftPad) + pLine
		}
	}

	return strings.Join(out, "\n")
}

func (m Model) renderThemePopup(boxWidth int) string {
	var lines []string
	title := "SWITCH THEME"
	lines = append(lines, renderBoxTop(title, boxWidth))

	for idx, th := range theme.Presets {
		cursor := "  "
		style := normalRowStyle
		if idx == m.popupCursor%len(theme.Presets) {
			cursor = "▶ "
			style = selectedRowStyle
		}
		activeTag := ""
		if th.ID == m.currentTheme.ID {
			activeTag = " [ACTIVE]"
		}
		line := fmt.Sprintf("%s%s%s", cursor, th.Name, activeTag)
		lines = append(lines, renderBoxLine(style.Render(line), boxWidth))
	}

	lines = append(lines, renderBoxLine("", boxWidth))
	hint := " [Enter] Select │ [Esc] Cancel │ [j/k] Move "
	lines = append(lines, renderBoxLine(dimStyle.Render(hint), boxWidth))
	lines = append(lines, renderBoxBottom(boxWidth))
	return strings.Join(lines, "\n")
}

func (m Model) renderActionsPopup(boxWidth int) string {
	var lines []string
	titleText := "⚡ ACTIONS"
	if m.popupActionTrack != nil {
		titleText = fmt.Sprintf("⚡ ACTIONS: %s", truncate(m.popupActionTrack.Title, 22))
	}
	lines = append(lines, renderBoxTop(titleText, boxWidth))

	options := []string{
		"▶ Play Now",
		"+ Add to Queue",
		"★ Toggle Favorite",
		"≡ Add to Playlist...",
		"☁ Save to NAS Cloud",
		"🔍 Filter / Browse Artist",
		"📋 Copy Link / Path",
	}

	for idx, opt := range options {
		cursor := "  "
		style := normalRowStyle
		if idx == m.popupCursor%len(options) {
			cursor = "▶ "
			style = selectedRowStyle
		}
		line := fmt.Sprintf("%s%s", cursor, opt)
		lines = append(lines, renderBoxLine(style.Render(line), boxWidth))
	}

	lines = append(lines, renderBoxLine("", boxWidth))
	hint := " [Enter] Execute │ [Esc] Cancel "
	lines = append(lines, renderBoxLine(dimStyle.Render(hint), boxWidth))
	lines = append(lines, renderBoxBottom(boxWidth))
	return strings.Join(lines, "\n")
}

func (m Model) renderHelpPopup(boxWidth int) string {
	var lines []string
	title := "⌨ KEYBOARD SHORTCUTS (spotify-player parity)"
	lines = append(lines, renderBoxTop(title, boxWidth))

	shortcuts := []string{
		"[Space]          Play / Pause",
		"[n] / [p]        Next / Previous track",
		"[s]              Toggle shuffle",
		"[r]              Cycle repeat mode",
		"[f]              Toggle favorite",
		"[.]              Play random track (shuffle jump)",
		"[_]              Mute / Unmute audio",
		"[^]              Seek to start (00:00)",
		"[>] / [<]        Seek forward / backward 10s",
		"[+] / [-]        Volume up / down (5%)",
		"[ [ ] / [ ] ]    Adjust lyric sync delay (±250ms)",
		"[j/k], [↑/↓]     Navigate track lists",
		"[g g] / [Home]   Jump to top",
		"[G] / [End]      Jump to bottom",
		"[C-f] / [C-b]    Page down / Page up",
		"[o] / [g a]      Actions popup on selected track",
		"[a]              Actions popup on current track",
		"[t] / [T]        Open theme switcher popup",
		"[1-9]            Switch view tabs (1..9)",
		"[z]              Jump to Queue tab",
		"[Z] / [C-z]      Add selected track to queue",
		"[d] / [C]        (In Queue) Delete item / Clear queue",
		"[K] / [J]        (In Queue) Move item up / down",
		"[S] / [s t/a/d]  Sort by Title, Artist, Duration, Reverse",
		"[v]              Cycle image mode (Halfblock/Braille/Sixel/Off)",
		"[w]              Open full-resolution cover window",
		"[/]              Unified search across all sources",
		"[q] / [Esc]      Back / Close popup / Clear search / Quit",
	}

	for _, s := range shortcuts {
		lines = append(lines, renderBoxLine(" "+s, boxWidth))
	}

	lines = append(lines, renderBoxLine("", boxWidth))
	lines = append(lines, renderBoxLine(dimStyle.Render(" Press [Esc] or [q] to close "), boxWidth))
	lines = append(lines, renderBoxBottom(boxWidth))
	return strings.Join(lines, "\n")
}

func (m Model) renderPlaylistSelectPopup(boxWidth int) string {
	var lines []string
	lines = append(lines, renderBoxTop("📁 ADD TO PLAYLIST", boxWidth))

	lists, _ := m.svc.DB.GetPlaylists()
	if len(lists) == 0 {
		lines = append(lines, renderBoxLine(" No playlists found. Create one in Playlists tab.", boxWidth))
	} else {
		for idx, l := range lists {
			cursor := "  "
			style := normalRowStyle
			if idx == m.popupCursor%len(lists) {
				cursor = "▶ "
				style = selectedRowStyle
			}
			line := fmt.Sprintf("%s%s (%d tracks)", cursor, l.Name, len(l.Tracks))
			lines = append(lines, renderBoxLine(style.Render(line), boxWidth))
		}
	}

	lines = append(lines, renderBoxLine("", boxWidth))
	lines = append(lines, renderBoxLine(dimStyle.Render(" [Enter] Add │ [Esc] Cancel "), boxWidth))
	lines = append(lines, renderBoxBottom(boxWidth))
	return strings.Join(lines, "\n")
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

	col1Width := 34
	if totalWidth < 80 {
		col1Width = 30
	}

	metaLines := m.renderMetadataCol(cur, isPlaying)
	posSec, _ := m.svc.Audio.Progress()
	lyricLines := m.renderLyricsCol(cur, posSec)

	showThumbMiddle := m.showThumbnail && totalWidth >= 80
	thumbWidth := 22
	thumbHeight := 11

	var thumbLines []string
	if showThumbMiddle {
		if cur != nil {
			if len(m.thumbnailLines) == thumbHeight {
				thumbLines = m.thumbnailLines
			} else if cached := metadata.GetCachedThumbnailProto(cur, thumbWidth, thumbHeight, m.imageProtocol); len(cached) == thumbHeight {
				thumbLines = cached
			} else {
				thumbLines = metadata.GenerateFallbackArtwork(thumbWidth, thumbHeight, cur.Title, cur.Artist)
			}
		} else {
			thumbLines = metadata.GenerateFallbackArtwork(thumbWidth, thumbHeight, "Rhythm", "")
		}
	}

	colLyricsWidth := totalWidth - col1Width - 3
	if showThumbMiddle {
		colLyricsWidth = totalWidth - col1Width - thumbWidth - 6
	}
	showLyrics := colLyricsWidth >= 16

	maxLines := len(metaLines)
	if showLyrics && len(lyricLines) > maxLines {
		maxLines = len(lyricLines)
	}
	if showThumbMiddle && len(thumbLines) > maxLines {
		maxLines = len(thumbLines)
	}

	colDivider := lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Secondary)).Render(" │ ")

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

		if showThumbMiddle {
			lt := ""
			if i < len(thumbLines) {
				lt = thumbLines[i]
			}
			wt := lipgloss.Width(lt)
			padT := thumbWidth - wt
			if padT < 0 {
				padT = 0
			}

			if showLyrics {
				l2 := ""
				if i < len(lyricLines) {
					l2 = lyricLines[i]
				}
				if lipgloss.Width(l2) > colLyricsWidth {
					l2 = lipgloss.NewStyle().MaxWidth(colLyricsWidth).Render(l2)
				}
				w2 := lipgloss.Width(l2)
				pad2 := colLyricsWidth - w2
				if pad2 < 0 {
					pad2 = 0
				}

				sb.WriteString(l1 + strings.Repeat(" ", pad1) + colDivider +
					lt + strings.Repeat(" ", padT) + colDivider +
					l2 + strings.Repeat(" ", pad2) + "\n")
			} else {
				sb.WriteString(l1 + strings.Repeat(" ", pad1) + colDivider +
					lt + strings.Repeat(" ", padT) + "\n")
			}
		} else {
			if showLyrics {
				l2 := ""
				if i < len(lyricLines) {
					l2 = lyricLines[i]
				}
				if lipgloss.Width(l2) > colLyricsWidth {
					l2 = lipgloss.NewStyle().MaxWidth(colLyricsWidth).Render(l2)
				}
				w2 := lipgloss.Width(l2)
				pad2 := colLyricsWidth - w2
				if pad2 < 0 {
					pad2 = 0
				}

				sb.WriteString(l1 + strings.Repeat(" ", pad1) + colDivider +
					l2 + strings.Repeat(" ", pad2) + "\n")
			} else {
				sb.WriteString(l1 + strings.Repeat(" ", pad1) + "\n")
			}
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

	kStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Accent)).Bold(true)
	cStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Subtext))

	row := func(label string, valStyled string) string {
		return kStyle.Render(label) + cStyle.Render(" : ") + valStyled
	}

	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Fg))

	titleVal := lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Fg)).Bold(true).Render(truncate(title, 22))
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
			barColor = currentThemeTheme.Accent
		} else if idx < 6 {
			barColor = currentThemeTheme.Secondary
		} else {
			barColor = currentThemeTheme.Playing
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

	posDuration := time.Duration(posSec*float64(time.Second)) + m.lyricOffset

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
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Secondary)).Render("Artist: " + truncate(cur.Artist, 28)),
			"",
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render(spinnerChar + " Syncing lyrics from LRCLIB..."),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Subtext)).Render("Querying synchronized timestamps"),
			"", "", "", "", "", "",
		}
	}

	if lyricsObj.IsFallback {
		return []string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Secondary)).Render("Artist: " + truncate(cur.Artist, 28)),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Playing)).Bold(true).Render("LRCLIB: NO MATCH"),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Subtext)).Render("No online synchronized lyrics found"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Subtext)).Render("Drop a .lrc file in folder for offline sync"),
			"", "", "", "", "",
		}
	}

	lines := lyricsObj.Lines
	if len(lines) == 0 {
		return []string{
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render("♪ " + truncate(cur.Title, 28) + " ♪"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Secondary)).Render("Artist: " + truncate(cur.Artist, 28)),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Subtext)).Render("No lyrics available for this track"),
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

	offsetHint := ""
	if m.lyricOffset != 0 {
		offsetHint = fmt.Sprintf(" (%+v)", m.lyricOffset)
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render("♪ " + truncate(cur.Title, 35) + offsetHint)

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
			waveStr.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Accent)).Render(string(r)))
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
	controls := fmt.Sprintf("[ ◀◀ ] %s [ ▶▶ ]  VOLUME: [%s] %d%%", playBtn, volBar, vol)

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
		content = "Press '/' to search, [o] actions, [T] themes, [?] shortcuts"
		rightHint = "[?] Help │ [T] Theme │ [o] Actions"
	}

	top := renderBoxTop("SEARCH / COMMAND", boxWidth)

	innerWidth := boxWidth - 2
	leftStr := prompt + content
	if m.searching {
		leftStr = prompt + cyanStyle.Render(content)
	} else if m.unifiedQuery != "" {
		leftStr = prompt + lipgloss.NewStyle().Foreground(lipgloss.Color(currentThemeTheme.Fg)).Bold(true).Render(content)
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
		title = fmt.Sprintf("SEARCH: '%s'", truncate(m.unifiedQuery, 16))
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
				filterPills[idx] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentThemeTheme.SelectedFg)).Background(lipgloss.Color(currentThemeTheme.SelectedBg)).Render(p)
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
		lines = append(lines, renderBoxLine(centerText("ADD TRACKS TO QUEUE (Press 'a' / 'Z')", width-2), width))
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
	lines = append(lines, renderBoxLine(fmt.Sprintf(" Audio Volume: %d%% │ Cache: %s │ Theme: %s", m.svc.Config.Audio.Volume, m.svc.Cache.FormattedStats(), m.currentTheme.Name), width))

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
