package theme

import (
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	ID         string
	Name       string
	Bg         string
	Fg         string
	Border     string
	SelectedBg string
	SelectedFg string
	Accent     string
	Secondary  string
	Subtext    string
	Playing    string
	Upcoming   string
	Played     string
}

var Presets = []Theme{
	{
		ID:         "tokyo-night",
		Name:       "Tokyo Night",
		Bg:         "#1a1b26",
		Fg:         "#c0caf5",
		Border:     "#3b4261",
		SelectedBg: "#283457",
		SelectedFg: "#ffffff",
		Accent:     "#7aa2f7",
		Secondary:  "#bb9af7",
		Subtext:    "#565f89",
		Playing:    "#f43f5e",
		Upcoming:   "#93c5fd",
		Played:     "#64748b",
	},
	{
		ID:         "catppuccin-mocha",
		Name:       "Catppuccin Mocha",
		Bg:         "#1e1e2e",
		Fg:         "#cdd6f4",
		Border:     "#45475a",
		SelectedBg: "#313244",
		SelectedFg: "#cba6f7",
		Accent:     "#89b4fa",
		Secondary:  "#f5c2e7",
		Subtext:    "#6c7086",
		Playing:    "#f38ba8",
		Upcoming:   "#b4befe",
		Played:     "#585b70",
	},
	{
		ID:         "dracula",
		Name:       "Dracula",
		Bg:         "#282a36",
		Fg:         "#f8f8f2",
		Border:     "#6272a4",
		SelectedBg: "#44475a",
		SelectedFg: "#50fa7b",
		Accent:     "#bd93f9",
		Secondary:  "#ff79c6",
		Subtext:    "#6272a4",
		Playing:    "#ff5555",
		Upcoming:   "#8be9fd",
		Played:     "#505a74",
	},
	{
		ID:         "gruvbox-dark",
		Name:       "Gruvbox Dark",
		Bg:         "#282828",
		Fg:         "#ebdbb2",
		Border:     "#504945",
		SelectedBg: "#3c3836",
		SelectedFg: "#fabd2f",
		Accent:     "#83a598",
		Secondary:  "#d3869b",
		Subtext:    "#928374",
		Playing:    "#fb4934",
		Upcoming:   "#b8bb26",
		Played:     "#665c54",
	},
	{
		ID:         "nord",
		Name:       "Nord",
		Bg:         "#2e3440",
		Fg:         "#eceff4",
		Border:     "#4c566a",
		SelectedBg: "#3b4252",
		SelectedFg: "#88c0d0",
		Accent:     "#81a1c1",
		Secondary:  "#b48ead",
		Subtext:    "#616e88",
		Playing:    "#bf616a",
		Upcoming:   "#8fbcbb",
		Played:     "#4c566a",
	},
	{
		ID:         "cyberpunk",
		Name:       "Cyberpunk 2077",
		Bg:         "#080811",
		Fg:         "#00f0ff",
		Border:     "#ff0055",
		SelectedBg: "#1a0b2e",
		SelectedFg: "#ffee00",
		Accent:     "#00f0ff",
		Secondary:  "#ff0077",
		Subtext:    "#7928ca",
		Playing:    "#ffee00",
		Upcoming:   "#00f0ff",
		Played:     "#4b1464",
	},
	{
		ID:         "rose-pine",
		Name:       "Rosé Pine",
		Bg:         "#191724",
		Fg:         "#e0def4",
		Border:     "#403d52",
		SelectedBg: "#26233a",
		SelectedFg: "#ebbcba",
		Accent:     "#9ccfd8",
		Secondary:  "#c4a7e7",
		Subtext:    "#6e6a86",
		Playing:    "#eb6f92",
		Upcoming:   "#31748f",
		Played:     "#524f67",
	},
	{
		ID:         "monokai",
		Name:       "Monokai Pro",
		Bg:         "#2d2a2e",
		Fg:         "#fcfcfa",
		Border:     "#5b595c",
		SelectedBg: "#403e41",
		SelectedFg: "#ffd866",
		Accent:     "#78dce8",
		Secondary:  "#ab9df2",
		Subtext:    "#727072",
		Playing:    "#ff6188",
		Upcoming:   "#a9dc76",
		Played:     "#494749",
	},
}

func GetTheme(id string) Theme {
	for _, t := range Presets {
		if t.ID == id {
			return t
		}
	}
	return Presets[0]
}

type Palette struct {
	Theme        Theme
	Header       lipgloss.Style
	NavActive    lipgloss.Style
	NavInactive  lipgloss.Style
	SelectedRow  lipgloss.Style
	NormalRow    lipgloss.Style
	Dim          lipgloss.Style
	Accent       lipgloss.Style
	Border       lipgloss.Style
	PopupBorder  lipgloss.Style
	PopupTitle   lipgloss.Style
	PlayingArrow lipgloss.Style
	LyricPlaying lipgloss.Style
	LyricPlayed  lipgloss.Style
	LyricNext    lipgloss.Style
	LyricUp      lipgloss.Style
}

func NewPalette(t Theme) Palette {
	return Palette{
		Theme: t,
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)).
			Bold(true),

		NavActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.SelectedFg)).
			Background(lipgloss.Color(t.SelectedBg)),

		NavInactive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Accent)),

		SelectedRow: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.SelectedFg)).
			Background(lipgloss.Color(t.SelectedBg)),

		NormalRow: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),

		Dim: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Subtext)),

		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Accent)),

		Border: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Border)),

		PopupBorder: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Secondary)).
			Bold(true),

		PopupTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.Accent)),

		PlayingArrow: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.Playing)),

		LyricPlaying: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.SelectedFg)).
			Background(lipgloss.Color(t.SelectedBg)),

		LyricPlayed: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Played)),

		LyricNext: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Upcoming)),

		LyricUp: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),
	}
}
