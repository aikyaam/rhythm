package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"rhythm/internal/core"
)

type AudioConfig struct {
	Volume  int    `json:"volume"`
	Shuffle bool   `json:"shuffle"`
	Repeat  string `json:"repeat"`
	Backend string `json:"backend"`
}

type LibraryConfig struct {
	Paths            []string `json:"paths"`
	AutoScanOnStart  bool     `json:"auto_scan_on_start"`
	WatchDirectories bool     `json:"watch_directories"`
}

type CacheConfig struct {
	Enabled   bool   `json:"enabled"`
	MaxSizeMB int64  `json:"max_size_mb"`
	CacheDir  string `json:"cache_dir"`
}

type KeybindingsConfig struct {
	PlayPause  string `json:"play_pause"`
	Next       string `json:"next"`
	Previous   string `json:"prev"`
	SeekFwd    string `json:"seek_fwd"`
	SeekBack   string `json:"seek_back"`
	VolumeUp   string `json:"volume_up"`
	VolumeDown string `json:"volume_down"`
	Search     string `json:"search"`
	Favorite   string `json:"favorite"`
	AddToQueue string `json:"add_to_queue"`
	SaveToNAS  string `json:"save_to_nas"`
	QueueView  string `json:"queue_view"`
	Help       string `json:"help"`
	Quit       string `json:"quit"`
}

type ImageConfig struct {
	Protocol string `json:"protocol"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type Config struct {
	Theme        string              `json:"theme"`
	Audio        AudioConfig         `json:"audio"`
	Library      LibraryConfig       `json:"library"`
	Cache        CacheConfig         `json:"cache"`
	Keybindings  KeybindingsConfig   `json:"keybindings"`
	Image        ImageConfig         `json:"image"`
	Servers      []core.ServerConfig `json:"servers"`
	ActiveServer string              `json:"active_server_id"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	musicDir := filepath.Join(home, "Music")
	cacheDir := filepath.Join(GetAppDir(), "cache")

	return &Config{
		Theme: "tokyo-night",
		Audio: AudioConfig{
			Volume:  80,
			Shuffle: false,
			Repeat:  string(core.RepeatOff),
			Backend: "auto",
		},
		Library: LibraryConfig{
			Paths:            []string{musicDir},
			AutoScanOnStart:  true,
			WatchDirectories: false,
		},
		Cache: CacheConfig{
			Enabled:   true,
			MaxSizeMB: 2048,
			CacheDir:  cacheDir,
		},
		Keybindings: KeybindingsConfig{
			PlayPause:  " ",
			Next:       "n",
			Previous:   "p",
			SeekFwd:    "right",
			SeekBack:   "left",
			VolumeUp:   "+",
			VolumeDown: "-",
			Search:     "/",
			Favorite:   "f",
			AddToQueue: "a",
			SaveToNAS:  "N",
			QueueView:  "q",
			Help:       "?",
			Quit:       "ctrl+c",
		},
		Image: ImageConfig{
			Protocol: "auto",
			Width:    22,
			Height:   11,
		},
		Servers:      []core.ServerConfig{},
		ActiveServer: "",
	}
}

func GetAppDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".rhythm")
	}
	dir := filepath.Join(configDir, "rhythm")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func GetConfigFilePath() string {
	return filepath.Join(GetAppDir(), "config.json")
}

func Load() (*Config, error) {
	path := GetConfigFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			_ = Save(cfg)
			return cfg, nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	for i, p := range cfg.Library.Paths {
		cfg.Library.Paths[i] = ExpandPath(p)
	}
	cfg.Cache.CacheDir = ExpandPath(cfg.Cache.CacheDir)

	return cfg, nil
}

func Save(cfg *Config) error {
	path := GetConfigFilePath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
