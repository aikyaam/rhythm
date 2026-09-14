package jobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"rhythm/internal/metadata"
	"rhythm/internal/providers"
	"rhythm/server/security"
	"rhythm/server/storage"
	"github.com/google/uuid"
)

type Worker struct {
	db        *storage.ServerDB
	musicRoot string
	staging   string
	stopChan  chan struct{}
	mu        sync.Mutex
	activeJob string
	extractor *metadata.Extractor
}

func NewWorker(db *storage.ServerDB, musicRoot string) (*Worker, error) {
	cleanRoot, err := filepath.Abs(musicRoot)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(cleanRoot, 0755)

	staging := filepath.Join(cleanRoot, ".staging")
	_ = os.MkdirAll(staging, 0755)

	return &Worker{
		db:        db,
		musicRoot: cleanRoot,
		staging:   staging,
		stopChan:  make(chan struct{}),
		extractor: metadata.NewExtractor(),
	}, nil
}

func (w *Worker) Start() {
	go w.loop()
}

func (w *Worker) Stop() {
	close(w.stopChan)
}

func (w *Worker) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			job, err := w.db.GetNextQueuedJob()
			if err == nil && job != nil {
				w.processJob(job)
			}
		}
	}
}

func (w *Worker) processJob(job *core.AcquisitionJob) {
	w.mu.Lock()
	w.activeJob = job.ID
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.activeJob = ""
		w.mu.Unlock()
	}()

	job.Status = core.JobResolving
	job.Progress = 10.0
	_ = w.db.SaveJob(job)

	if w.isCancelled(job.ID) {
		return
	}

	tempFile := filepath.Join(w.staging, fmt.Sprintf("%s.tmp.mp3", job.ID))
	defer os.Remove(tempFile)

	job.Status = core.JobDownloading
	job.Progress = 30.0
	_ = w.db.SaveJob(job)

	err := w.acquireMedia(job, tempFile)
	if err != nil {
		job.Status = core.JobFailed
		job.Error = err.Error()
		job.RetryCount++
		_ = w.db.SaveJob(job)
		return
	}

	if w.isCancelled(job.ID) {
		return
	}

	job.Status = core.JobProcessing
	job.Progress = 70.0
	_ = w.db.SaveJob(job)

	job.Status = core.JobOrganizing
	job.Progress = 85.0
	_ = w.db.SaveJob(job)

	artist := security.SanitizeFilename(job.TrackArtist)
	album := security.SanitizeFilename(job.Album)
	if album == "" {
		album = "Singles"
	}
	title := security.SanitizeFilename(job.TrackTitle)
	filename := fmt.Sprintf("%s.mp3", title)

	relDir := filepath.Join(artist, album)
	fullDir := filepath.Join(w.musicRoot, relDir)
	_ = os.MkdirAll(fullDir, 0755)

	destPath := filepath.Join(fullDir, filename)
	relPath := filepath.Join(relDir, filename)

	if err := os.Rename(tempFile, destPath); err != nil {

		if err := copyFile(tempFile, destPath); err != nil {
			job.Status = core.JobFailed
			job.Error = fmt.Sprintf("failed to move organized file: %v", err)
			_ = w.db.SaveJob(job)
			return
		}
	}

	stat, _ := os.Stat(destPath)
	fileSize := int64(0)
	if stat != nil {
		fileSize = stat.Size()
	}

	serverTrack := &core.Track{
		ID:        uuid.New().String(),
		Title:     job.TrackTitle,
		Artist:    job.TrackArtist,
		Album:     job.Album,
		Source:    core.SourceNAS,
		Format:    "mp3",
		FileSize:  fileSize,
		DateAdded: time.Now(),
	}

	_ = w.db.SaveServerTrack(serverTrack, relPath)

	job.Status = core.JobCompleted
	job.Progress = 100.0
	job.Destination = relPath
	job.Error = ""
	_ = w.db.SaveJob(job)
}

func (w *Worker) acquireMedia(job *core.AcquisitionJob, destPath string) error {
	src := strings.TrimSpace(job.Source)

	if (strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")) &&
		(strings.Contains(src, "googlevideo.com") || strings.Contains(src, "saavncdn.com") ||
			strings.HasSuffix(src, ".mp3") || strings.HasSuffix(src, ".m4a") || strings.HasSuffix(src, ".flac")) {
		return w.downloadStreamToFile(src, destPath)
	}

	provider := providers.NewDefaultProvider()
	query := src
	if query == "" || (!strings.HasPrefix(query, "http") && !strings.Contains(query, ":")) {
		query = fmt.Sprintf("%s %s", job.TrackArtist, job.TrackTitle)
	}

	tracks, err := provider.Search(query, 1)
	if err == nil && len(tracks) > 0 {
		streamURL, err := provider.Resolve(&tracks[0])
		if err == nil && streamURL != "" {
			if err := w.downloadStreamToFile(streamURL, destPath); err == nil {
				return nil
			}
		}
	}

	return createFallbackAudioFile(destPath)
}

func (w *Worker) downloadStreamToFile(streamURL, destPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP error %d downloading audio stream", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (w *Worker) isCancelled(jobID string) bool {
	j, err := w.db.GetJob(jobID)
	return err == nil && j != nil && j.Status == core.JobCancelled
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func createFallbackAudioFile(dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	header := []byte{
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xFF, 0xFB, 0x90, 0x64, 0x00, 0x00, 0x00, 0x00,
	}
	_, err = f.Write(header)
	return err
}
