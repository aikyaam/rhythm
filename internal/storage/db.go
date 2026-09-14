package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
	mu sync.RWMutex
}

func Open(dbPath string) (*DB, error) {
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".rhythm")
		_ = os.MkdirAll(dir, 0755)
		dbPath = filepath.Join(dir, "rhythm.db")
	} else {
		_ = os.MkdirAll(filepath.Dir(dbPath), 0755)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	conn.SetMaxOpenConns(1)

	d := &DB{db: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tracks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		artist TEXT NOT NULL,
		album TEXT,
		duration REAL,
		artwork_url TEXT,
		year INTEGER,
		genre TEXT,
		source TEXT NOT NULL,
		source_id TEXT,
		local_path TEXT,
		remote_reference TEXT,
		stream_url TEXT,
		server_id TEXT,
		format TEXT,
		file_size INTEGER,
		bitrate INTEGER,
		date_added TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist);
	CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album);
	CREATE INDEX IF NOT EXISTS idx_tracks_title ON tracks(title);
	CREATE INDEX IF NOT EXISTS idx_tracks_source ON tracks(source);

	CREATE TABLE IF NOT EXISTS playlists (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		created_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS playlist_tracks (
		id TEXT PRIMARY KEY,
		playlist_id TEXT NOT NULL,
		position INTEGER NOT NULL,
		track_id TEXT NOT NULL,
		title TEXT,
		artist TEXT,
		album TEXT,
		duration REAL,
		source TEXT,
		source_id TEXT,
		local_path TEXT,
		remote_reference TEXT,
		stream_url TEXT,
		server_id TEXT,
		FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS favorites (
		track_id TEXT PRIMARY KEY,
		title TEXT,
		artist TEXT,
		album TEXT,
		duration REAL,
		source TEXT,
		source_id TEXT,
		local_path TEXT,
		remote_reference TEXT,
		stream_url TEXT,
		server_id TEXT,
		added_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS history (
		id TEXT PRIMARY KEY,
		track_id TEXT,
		title TEXT,
		artist TEXT,
		album TEXT,
		duration REAL,
		source TEXT,
		source_id TEXT,
		played_at TIMESTAMP,
		duration_listened REAL
	);

	CREATE INDEX IF NOT EXISTS idx_history_played_at ON history(played_at DESC);

	CREATE TABLE IF NOT EXISTS servers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		username TEXT,
		password TEXT,
		token TEXT,
		use_tls INTEGER,
		music_dir TEXT,
		capabilities TEXT,
		is_active INTEGER,
		created_at TIMESTAMP
	);
	`

	_, err := d.db.Exec(schema)
	return err
}

func (d *DB) SaveTrack(t *core.Track) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
	INSERT INTO tracks (id, title, artist, album, duration, artwork_url, year, genre, source, source_id, local_path, remote_reference, stream_url, server_id, format, file_size, bitrate, date_added)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title=excluded.title,
		artist=excluded.artist,
		album=excluded.album,
		duration=excluded.duration,
		artwork_url=excluded.artwork_url,
		year=excluded.year,
		genre=excluded.genre,
		source=excluded.source,
		source_id=excluded.source_id,
		local_path=excluded.local_path,
		remote_reference=excluded.remote_reference,
		stream_url=excluded.stream_url,
		server_id=excluded.server_id,
		format=excluded.format,
		file_size=excluded.file_size,
		bitrate=excluded.bitrate;
	`

	if t.DateAdded.IsZero() {
		t.DateAdded = time.Now()
	}

	_, err := d.db.Exec(query,
		t.ID, t.Title, t.Artist, t.Album, t.Duration, t.ArtworkURL, t.Year, t.Genre,
		string(t.Source), t.SourceID, t.LocalPath, t.RemoteReference, t.StreamURL, t.ServerID,
		t.Format, t.FileSize, t.Bitrate, t.DateAdded,
	)
	return err
}

func (d *DB) GetAllTracks() ([]core.Track, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT id, title, artist, album, duration, artwork_url, year, genre, source, source_id, local_path, remote_reference, stream_url, server_id, format, file_size, bitrate, date_added FROM tracks ORDER BY artist, album, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []core.Track
	for rows.Next() {
		var t core.Track
		var src string
		var art, gn, lp, rr, su, srv, fmt sql.NullString
		var fs, br sql.NullInt64
		var yrInt sql.NullInt32
		var dur sql.NullFloat64

		err := rows.Scan(
			&t.ID, &t.Title, &t.Artist, &t.Album, &dur, &art, &yrInt, &gn,
			&src, &t.SourceID, &lp, &rr, &su, &srv, &fmt, &fs, &br, &t.DateAdded,
		)
		if err != nil {
			return nil, err
		}

		t.Duration = dur.Float64
		t.ArtworkURL = art.String
		if yrInt.Valid {
			t.Year = int(yrInt.Int32)
		}
		t.Genre = gn.String
		t.Source = core.SourceType(src)
		t.LocalPath = lp.String
		t.RemoteReference = rr.String
		t.StreamURL = su.String
		t.ServerID = srv.String
		t.Format = fmt.String
		t.FileSize = fs.Int64
		t.Bitrate = int(br.Int64)

		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (d *DB) SearchTracks(query string) ([]core.Track, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	pattern := "%" + strings.ToLower(query) + "%"
	q := `SELECT id, title, artist, album, duration, artwork_url, year, genre, source, source_id, local_path, remote_reference, stream_url, server_id, format, file_size, bitrate, date_added 
		  FROM tracks 
		  WHERE LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?
		  ORDER BY artist, album, title`

	rows, err := d.db.Query(q, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []core.Track
	for rows.Next() {
		var t core.Track
		var src string
		var art, gn, lp, rr, su, srv, fmt sql.NullString
		var fs, br sql.NullInt64
		var yrInt sql.NullInt32
		var dur sql.NullFloat64

		if err := rows.Scan(
			&t.ID, &t.Title, &t.Artist, &t.Album, &dur, &art, &yrInt, &gn,
			&src, &t.SourceID, &lp, &rr, &su, &srv, &fmt, &fs, &br, &t.DateAdded,
		); err != nil {
			return nil, err
		}

		t.Duration = dur.Float64
		t.ArtworkURL = art.String
		if yrInt.Valid {
			t.Year = int(yrInt.Int32)
		}
		t.Genre = gn.String
		t.Source = core.SourceType(src)
		t.LocalPath = lp.String
		t.RemoteReference = rr.String
		t.StreamURL = su.String
		t.ServerID = srv.String
		t.Format = fmt.String
		t.FileSize = fs.Int64
		t.Bitrate = int(br.Int64)

		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (d *DB) GetArtists() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT DISTINCT artist FROM tracks WHERE artist != '' ORDER BY artist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err == nil {
			artists = append(artists, a)
		}
	}
	return artists, nil
}

func (d *DB) GetAlbums() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT DISTINCT album FROM tracks WHERE album != '' ORDER BY album`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err == nil {
			albums = append(albums, a)
		}
	}
	return albums, nil
}

func (d *DB) AddFavorite(t *core.Track) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	q := `INSERT OR REPLACE INTO favorites (track_id, title, artist, album, duration, source, source_id, local_path, remote_reference, stream_url, server_id, added_at)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(q, t.ID, t.Title, t.Artist, t.Album, t.Duration, string(t.Source), t.SourceID, t.LocalPath, t.RemoteReference, t.StreamURL, t.ServerID, time.Now())
	return err
}

func (d *DB) RemoveFavorite(trackID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM favorites WHERE track_id = ?`, trackID)
	return err
}

func (d *DB) IsFavorite(trackID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	_ = d.db.QueryRow(`SELECT COUNT(1) FROM favorites WHERE track_id = ?`, trackID).Scan(&count)
	return count > 0
}

func (d *DB) GetFavorites() ([]core.Track, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT track_id, title, artist, album, duration, source, source_id, local_path, remote_reference, stream_url, server_id FROM favorites ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []core.Track
	for rows.Next() {
		var t core.Track
		var src string
		var lp, rr, su, srv sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &src, &t.SourceID, &lp, &rr, &su, &srv); err != nil {
			return nil, err
		}
		t.Source = core.SourceType(src)
		t.LocalPath = lp.String
		t.RemoteReference = rr.String
		t.StreamURL = su.String
		t.ServerID = srv.String
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func (d *DB) SaveHistoryEntry(entry *core.HistoryEntry) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	q := `INSERT INTO history (id, track_id, title, artist, album, duration, source, source_id, played_at, duration_listened)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(q, entry.ID, entry.TrackID, entry.Track.Title, entry.Track.Artist, entry.Track.Album, entry.Track.Duration, string(entry.Track.Source), entry.Track.SourceID, entry.PlayedAt, entry.Duration)
	return err
}

func (d *DB) GetRecentHistory(limit int) ([]core.HistoryEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	q := `SELECT id, track_id, title, artist, album, duration, source, source_id, played_at, duration_listened
		  FROM history ORDER BY played_at DESC LIMIT ?`
	rows, err := d.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []core.HistoryEntry
	for rows.Next() {
		var e core.HistoryEntry
		var src string
		if err := rows.Scan(&e.ID, &e.TrackID, &e.Track.Title, &e.Track.Artist, &e.Track.Album, &e.Track.Duration, &src, &e.Track.SourceID, &e.PlayedAt, &e.Duration); err != nil {
			return nil, err
		}
		e.Track.ID = e.TrackID
		e.Track.Source = core.SourceType(src)
		entries = append(entries, e)
	}
	return entries, nil
}

func (d *DB) SavePlaylist(p *core.Playlist) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO playlists (id, name, description, created_at)
					  VALUES (?, ?, ?, ?)
					  ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description`,
		p.ID, p.Name, p.Description, p.CreatedAt)
	if err != nil {
		return err
	}

	_, _ = tx.Exec(`DELETE FROM playlist_tracks WHERE playlist_id = ?`, p.ID)

	stmt, err := tx.Prepare(`INSERT INTO playlist_tracks (id, playlist_id, position, track_id, title, artist, album, duration, source, source_id, local_path, remote_reference, stream_url, server_id)
							 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for idx, t := range p.Tracks {
		rowID := fmt.Sprintf("%s-%d", p.ID, idx)
		_, err := stmt.Exec(rowID, p.ID, idx, t.ID, t.Title, t.Artist, t.Album, t.Duration, string(t.Source), t.SourceID, t.LocalPath, t.RemoteReference, t.StreamURL, t.ServerID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (d *DB) DeletePlaylist(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, _ = d.db.Exec(`DELETE FROM playlist_tracks WHERE playlist_id = ?`, id)
	_, err := d.db.Exec(`DELETE FROM playlists WHERE id = ?`, id)
	return err
}

func (d *DB) GetPlaylists() ([]core.Playlist, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT id, name, description, created_at FROM playlists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playlists []core.Playlist
	for rows.Next() {
		var p core.Playlist
		var desc sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &desc, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Description = desc.String
		playlists = append(playlists, p)
	}

	for i := range playlists {
		tracksRows, err := d.db.Query(`SELECT track_id, title, artist, album, duration, source, source_id, local_path, remote_reference, stream_url, server_id 
										FROM playlist_tracks WHERE playlist_id = ? ORDER BY position`, playlists[i].ID)
		if err == nil {
			for tracksRows.Next() {
				var t core.Track
				var src string
				var lp, rr, su, srv sql.NullString
				if err := tracksRows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &src, &t.SourceID, &lp, &rr, &su, &srv); err == nil {
					t.Source = core.SourceType(src)
					t.LocalPath = lp.String
					t.RemoteReference = rr.String
					t.StreamURL = su.String
					t.ServerID = srv.String
					playlists[i].Tracks = append(playlists[i].Tracks, t)
				}
			}
			tracksRows.Close()
		}
	}

	return playlists, nil
}

func (d *DB) SaveServer(s *core.ServerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	q := `INSERT INTO servers (id, name, host, port, username, password, token, use_tls, music_dir, capabilities, is_active, created_at)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		  ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, host=excluded.host, port=excluded.port, username=excluded.username,
			password=excluded.password, token=excluded.token, use_tls=excluded.use_tls,
			music_dir=excluded.music_dir, capabilities=excluded.capabilities, is_active=excluded.is_active`

	tlsInt := 0
	if s.UseTLS {
		tlsInt = 1
	}
	activeInt := 0
	if s.IsActive {
		activeInt = 1
	}
	caps := strings.Join(s.Capabilities, ",")

	_, err := d.db.Exec(q, s.ID, s.Name, s.Host, s.Port, s.Username, s.Password, s.Token, tlsInt, s.MusicDir, caps, activeInt, s.CreatedAt)
	return err
}

func (d *DB) GetServers() ([]core.ServerConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT id, name, host, port, username, password, token, use_tls, music_dir, capabilities, is_active, created_at FROM servers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []core.ServerConfig
	for rows.Next() {
		var s core.ServerConfig
		var u, p, tok, md, caps sql.NullString
		var tlsInt, actInt int
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &u, &p, &tok, &tlsInt, &md, &caps, &actInt, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Username = u.String
		s.Password = p.String
		s.Token = tok.String
		s.UseTLS = (tlsInt == 1)
		s.MusicDir = md.String
		s.IsActive = (actInt == 1)
		if caps.Valid && caps.String != "" {
			s.Capabilities = strings.Split(caps.String, ",")
		}
		servers = append(servers, s)
	}
	return servers, nil
}

func (d *DB) DeleteServer(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM servers WHERE id = ?`, id)
	return err
}
