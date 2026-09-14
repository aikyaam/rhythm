package core

import (
	"fmt"
	"time"
)

type SourceType string

const (
	SourceLocal  SourceType = "LOCAL"
	SourceOnline SourceType = "ONLINE"
	SourceNAS    SourceType = "NAS"
)

type PlaybackState string

const (
	StateIdle    PlaybackState = "IDLE"
	StatePlaying PlaybackState = "PLAYING"
	StatePaused  PlaybackState = "PAUSED"
	StateStopped PlaybackState = "STOPPED"
)

type RepeatMode string

const (
	RepeatOff RepeatMode = "OFF"
	RepeatOne RepeatMode = "ONE"
	RepeatAll RepeatMode = "ALL"
)

type JobStatus string

const (
	JobQueued      JobStatus = "QUEUED"
	JobResolving   JobStatus = "RESOLVING"
	JobDownloading JobStatus = "DOWNLOADING"
	JobProcessing  JobStatus = "PROCESSING"
	JobOrganizing  JobStatus = "ORGANIZING"
	JobCompleted   JobStatus = "COMPLETED"
	JobFailed      JobStatus = "FAILED"
	JobCancelled   JobStatus = "CANCELLED"
)

type Track struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Artist          string     `json:"artist"`
	Album           string     `json:"album"`
	Duration        float64    `json:"duration"`
	ArtworkURL      string     `json:"artwork_url,omitempty"`
	Year            int        `json:"year,omitempty"`
	Genre           string     `json:"genre,omitempty"`
	Source          SourceType `json:"source"`
	SourceID        string     `json:"source_id"`
	LocalPath       string     `json:"local_path,omitempty"`
	RemoteReference string     `json:"remote_reference,omitempty"`
	StreamURL       string     `json:"stream_url,omitempty"`
	ServerID        string     `json:"server_id,omitempty"`
	Format          string     `json:"format,omitempty"`
	FileSize        int64      `json:"file_size,omitempty"`
	Bitrate         int        `json:"bitrate,omitempty"`
	DateAdded       time.Time  `json:"date_added"`
}

func (t Track) DisplayDuration() string {
	mins := int(t.Duration) / 60
	secs := int(t.Duration) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

type ServerConfig struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Username     string    `json:"username,omitempty"`
	Password     string    `json:"password,omitempty"`
	Token        string    `json:"token,omitempty"`
	UseTLS       bool      `json:"use_tls"`
	MusicDir     string    `json:"music_dir,omitempty"`
	Capabilities []string  `json:"capabilities"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s ServerConfig) BaseURL() string {
	scheme := "http"
	if s.UseTLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, s.Host, s.Port)
}

type AcquisitionJob struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	TrackTitle  string    `json:"track_title"`
	TrackArtist string    `json:"track_artist"`
	Album       string    `json:"album,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Status      JobStatus `json:"status"`
	Progress    float64   `json:"progress"`
	Error       string    `json:"error,omitempty"`
	RetryCount  int       `json:"retry_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Playlist struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Tracks      []Track   `json:"tracks"`
}

type QueueItem struct {
	ID      string    `json:"id"`
	Track   Track     `json:"track"`
	AddedAt time.Time `json:"added_at"`
}

type HistoryEntry struct {
	ID       string    `json:"id"`
	TrackID  string    `json:"track_id"`
	Track    Track     `json:"track"`
	PlayedAt time.Time `json:"played_at"`
	Duration float64   `json:"duration"`
}

type ServerStatus struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	UptimeSeconds  int64    `json:"uptime_seconds"`
	TotalTracks    int      `json:"total_tracks"`
	StorageTotalMB int64    `json:"storage_total_mb"`
	StorageFreeMB  int64    `json:"storage_free_mb"`
	ActiveJobs     int      `json:"active_jobs"`
	Capabilities   []string `json:"capabilities"`
}
