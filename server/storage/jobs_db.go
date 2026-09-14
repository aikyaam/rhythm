package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"time"

	"rhythm/internal/core"
	_ "modernc.org/sqlite"
)

type ServerDB struct {
	db *sql.DB
	mu sync.RWMutex
}

func OpenServerDB(dbPath string) (*ServerDB, error) {
	_ = os.MkdirAll(filepath.Dir(dbPath), 0755)

	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	conn.SetMaxOpenConns(1)

	s := &ServerDB{db: conn}
	if err := s.migrate(); err != nil {
		conn.Close()
		return nil, err
	}

	s.recoverInterruptedJobs()

	return s, nil
}

func (s *ServerDB) Close() error {
	return s.db.Close()
}

func (s *ServerDB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS acquisition_jobs (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		track_title TEXT NOT NULL,
		track_artist TEXT NOT NULL,
		album TEXT,
		destination TEXT,
		status TEXT NOT NULL,
		progress REAL NOT NULL,
		error TEXT,
		retry_count INTEGER DEFAULT 0,
		created_at TIMESTAMP,
		updated_at TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_status ON acquisition_jobs(status);

	CREATE TABLE IF NOT EXISTS server_tracks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		artist TEXT NOT NULL,
		album TEXT,
		duration REAL,
		format TEXT,
		file_size INTEGER,
		rel_path TEXT NOT NULL UNIQUE,
		date_added TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_srv_tracks_artist ON server_tracks(artist);
	CREATE INDEX IF NOT EXISTS idx_srv_tracks_album ON server_tracks(album);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *ServerDB) recoverInterruptedJobs() {

	_, _ = s.db.Exec(`
		UPDATE acquisition_jobs 
		SET status = 'QUEUED', updated_at = ? 
		WHERE status IN ('RESOLVING', 'DOWNLOADING', 'PROCESSING', 'ORGANIZING')
	`, time.Now())
}

func (s *ServerDB) SaveJob(job *core.AcquisitionJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job.UpdatedAt = time.Now()
	q := `INSERT INTO acquisition_jobs (id, source, track_title, track_artist, album, destination, status, progress, error, retry_count, created_at, updated_at)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		  ON CONFLICT(id) DO UPDATE SET
			source=excluded.source, track_title=excluded.track_title, track_artist=excluded.track_artist,
			album=excluded.album, destination=excluded.destination, status=excluded.status,
			progress=excluded.progress, error=excluded.error, retry_count=excluded.retry_count,
			updated_at=excluded.updated_at`

	_, err := s.db.Exec(q, job.ID, job.Source, job.TrackTitle, job.TrackArtist, job.Album, job.Destination,
		string(job.Status), job.Progress, job.Error, job.RetryCount, job.CreatedAt, job.UpdatedAt)
	return err
}

func (s *ServerDB) GetJob(id string) (*core.AcquisitionJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := `SELECT id, source, track_title, track_artist, album, destination, status, progress, error, retry_count, created_at, updated_at
		  FROM acquisition_jobs WHERE id = ?`

	var j core.AcquisitionJob
	var st string
	var errStr, alb, dest sql.NullString
	err := s.db.QueryRow(q, id).Scan(
		&j.ID, &j.Source, &j.TrackTitle, &j.TrackArtist, &alb, &dest,
		&st, &j.Progress, &errStr, &j.RetryCount, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	j.Album = alb.String
	j.Destination = dest.String
	j.Status = core.JobStatus(st)
	j.Error = errStr.String
	return &j, nil
}

func (s *ServerDB) GetAllJobs() ([]core.AcquisitionJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, source, track_title, track_artist, album, destination, status, progress, error, retry_count, created_at, updated_at
							 FROM acquisition_jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []core.AcquisitionJob
	for rows.Next() {
		var j core.AcquisitionJob
		var st string
		var errStr, alb, dest sql.NullString
		if err := rows.Scan(&j.ID, &j.Source, &j.TrackTitle, &j.TrackArtist, &alb, &dest, &st, &j.Progress, &errStr, &j.RetryCount, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		j.Album = alb.String
		j.Destination = dest.String
		j.Status = core.JobStatus(st)
		j.Error = errStr.String
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *ServerDB) GetNextQueuedJob() (*core.AcquisitionJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := `SELECT id, source, track_title, track_artist, album, destination, status, progress, error, retry_count, created_at, updated_at
		  FROM acquisition_jobs WHERE status = 'QUEUED' ORDER BY created_at ASC LIMIT 1`

	var j core.AcquisitionJob
	var st string
	var errStr, alb, dest sql.NullString
	err := s.db.QueryRow(q).Scan(
		&j.ID, &j.Source, &j.TrackTitle, &j.TrackArtist, &alb, &dest,
		&st, &j.Progress, &errStr, &j.RetryCount, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	j.Album = alb.String
	j.Destination = dest.String
	j.Status = core.JobStatus(st)
	j.Error = errStr.String
	return &j, nil
}

func (s *ServerDB) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM acquisition_jobs WHERE id = ?`, id)
	return err
}

func (s *ServerDB) SaveServerTrack(t *core.Track, relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := `INSERT INTO server_tracks (id, title, artist, album, duration, format, file_size, rel_path, date_added)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		  ON CONFLICT(rel_path) DO UPDATE SET
			title=excluded.title, artist=excluded.artist, album=excluded.album,
			duration=excluded.duration, format=excluded.format, file_size=excluded.file_size`

	_, err := s.db.Exec(q, t.ID, t.Title, t.Artist, t.Album, t.Duration, t.Format, t.FileSize, relPath, time.Now())
	return err
}

func (s *ServerDB) GetServerTracks(query string) ([]core.Track, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sqlQuery := `SELECT id, title, artist, album, duration, format, file_size, rel_path, date_added FROM server_tracks`
	var rows *sql.Rows
	var err error

	if query != "" {
		sqlQuery += ` WHERE LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ? ORDER BY artist, album, title`
		p := "%" + query + "%"
		rows, err = s.db.Query(sqlQuery, p, p, p)
	} else {
		sqlQuery += ` ORDER BY artist, album, title`
		rows, err = s.db.Query(sqlQuery)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []core.Track
	for rows.Next() {
		var t core.Track
		var relPath string
		var alb sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &alb, &t.Duration, &t.Format, &t.FileSize, &relPath, &t.DateAdded); err != nil {
			return nil, err
		}
		t.Album = alb.String
		t.Source = core.SourceNAS
		t.SourceID = t.ID
		t.RemoteReference = relPath
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func (s *ServerDB) GetServerTrackByID(id string) (*core.Track, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := `SELECT id, title, artist, album, duration, format, file_size, rel_path, date_added FROM server_tracks WHERE id = ?`
	var t core.Track
	var relPath string
	var alb sql.NullString

	err := s.db.QueryRow(q, id).Scan(&t.ID, &t.Title, &t.Artist, &alb, &t.Duration, &t.Format, &t.FileSize, &relPath, &t.DateAdded)
	if err != nil {
		return nil, "", err
	}
	t.Album = alb.String
	t.Source = core.SourceNAS
	t.SourceID = t.ID
	t.RemoteReference = relPath
	return &t, relPath, nil
}

func (s *ServerDB) TotalTrackCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM server_tracks`).Scan(&count)
	return count
}
