package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"rhythm/server/jobs"
	"rhythm/server/storage"
	"rhythm/server/streaming"
	"github.com/google/uuid"
)

type Server struct {
	db          *storage.ServerDB
	worker      *jobs.Worker
	streamer    *streaming.Handler
	musicRoot   string
	startTime   time.Time
	tokenSecret string
	tokens      map[string]string
	mu          sync.RWMutex

	Username string
	Password string
	Port     int
}

func NewServer(musicRoot, dbPath, username, password string, port int) (*Server, error) {
	db, err := storage.OpenServerDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open server db: %w", err)
	}

	worker, err := jobs.NewWorker(db, musicRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize worker: %w", err)
	}

	streamer := streaming.NewHandler(musicRoot)

	s := &Server{
		db:          db,
		worker:      worker,
		streamer:    streamer,
		musicRoot:   musicRoot,
		startTime:   time.Now(),
		tokens:      make(map[string]string),
		Username:    username,
		Password:    password,
		Port:        port,
		tokenSecret: generateRandomToken(),
	}

	return s, nil
}

func generateRandomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) Start() error {
	s.worker.Start()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)

	mux.HandleFunc("/api/v1/system/status", s.requireAuth(s.handleSystemStatus))
	mux.HandleFunc("/api/v1/library/tracks", s.requireAuth(s.handleLibraryTracks))
	mux.HandleFunc("/api/v1/library/refresh", s.requireAuth(s.handleLibraryRefresh))
	mux.HandleFunc("/api/v1/stream/", s.requireAuth(s.handleStream))
	mux.HandleFunc("/api/v1/jobs", s.requireAuth(s.handleJobs))
	mux.HandleFunc("/api/v1/jobs/", s.requireAuth(s.handleJobDetail))

	addr := fmt.Sprintf(":%d", s.Port)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) Stop() {
	s.worker.Stop()
	_ = s.db.Close()
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		if s.Password == "" {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {

			token := r.URL.Query().Get("token")
			if token == "" || !s.isValidToken(token) {
				http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if !s.isValidToken(token) {
			http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (s *Server) isValidToken(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.tokens[token]
	return exists
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := core.ServerStatus{
		Name:          "Rhythm NAS Server",
		Version:       "1.0.0",
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		TotalTracks:   s.db.TotalTrackCount(),
		Capabilities:  []string{"streaming", "acquisition", "library"},
	}
	writeJSON(w, http.StatusOK, status)
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token        string   `json:"token"`
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if s.Username != "" && req.Username != s.Username {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	if s.Password != "" && req.Password != s.Password {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token := generateRandomToken()
	s.mu.Lock()
	s.tokens[token] = req.Username
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, LoginResponse{
		Token:        token,
		Capabilities: []string{"streaming", "acquisition", "library"},
	})
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	status := core.ServerStatus{
		Name:           "Rhythm NAS Server",
		Version:        "1.0.0",
		UptimeSeconds:  int64(time.Since(s.startTime).Seconds()),
		TotalTracks:    s.db.TotalTrackCount(),
		StorageTotalMB: 100000,
		StorageFreeMB:  85000,
		Capabilities:   []string{"streaming", "acquisition", "library"},
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleLibraryTracks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	tracks, err := s.db.GetServerTracks(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tracks == nil {
		tracks = make([]core.Track, 0)
	}
	writeJSON(w, http.StatusOK, tracks)
}

func (s *Server) handleLibraryRefresh(w http.ResponseWriter, r *http.Request) {

	count := 0
	_ = filepath.Walk(s.musicRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".mp3" || ext == ".flac" || ext == ".wav" {
				rel, _ := filepath.Rel(s.musicRoot, path)
				base := strings.TrimSuffix(filepath.Base(path), ext)
				t := &core.Track{
					ID:        uuid.New().String(),
					Title:     base,
					Artist:    "NAS Library",
					Format:    strings.TrimPrefix(ext, "."),
					FileSize:  info.Size(),
					DateAdded: time.Now(),
				}
				_ = s.db.SaveServerTrack(t, rel)
				count++
			}
		}
		return nil
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"scanned": count,
	})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {

	trackID := strings.TrimPrefix(r.URL.Path, "/api/v1/stream/")
	if trackID == "" {
		http.Error(w, "track_id required", http.StatusBadRequest)
		return
	}

	_, relPath, err := s.db.GetServerTrackByID(trackID)
	if err != nil || relPath == "" {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	s.streamer.ServeAudio(w, r, relPath)
}

type CreateJobRequest struct {
	Source      string `json:"source"`
	TrackTitle  string `json:"track_title"`
	TrackArtist string `json:"track_artist"`
	Album       string `json:"album"`
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := s.db.GetAllJobs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if jobs == nil {
			jobs = make([]core.AcquisitionJob, 0)
		}
		writeJSON(w, http.StatusOK, jobs)

	case http.MethodPost:
		var req CreateJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}

		if req.TrackTitle == "" {
			req.TrackTitle = "Acquired Track"
		}
		if req.TrackArtist == "" {
			req.TrackArtist = "Unknown Artist"
		}

		job := &core.AcquisitionJob{
			ID:          uuid.New().String(),
			Source:      req.Source,
			TrackTitle:  req.TrackTitle,
			TrackArtist: req.TrackArtist,
			Album:       req.Album,
			Status:      core.JobQueued,
			Progress:    0.0,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := s.db.SaveJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusAccepted, job)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
	parts := strings.Split(sub, "/")
	jobID := parts[0]

	if jobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJob(jobID)
	if err != nil || job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, job)
		case http.MethodDelete:
			_ = s.db.DeleteJob(jobID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	action := parts[1]
	switch action {
	case "cancel":
		if r.Method == http.MethodPost {
			job.Status = core.JobCancelled
			_ = s.db.SaveJob(job)
			writeJSON(w, http.StatusOK, job)
			return
		}
	case "retry":
		if r.Method == http.MethodPost {
			job.Status = core.JobQueued
			job.Progress = 0.0
			job.Error = ""
			_ = s.db.SaveJob(job)
			writeJSON(w, http.StatusOK, job)
			return
		}
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
