package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"rhythm/internal/core"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type ConnectionTestResult struct {
	Reachable            bool
	Authenticated        bool
	LibraryAvailable     bool
	StreamingSupported   bool
	AcquisitionSupported bool
	ServerName           string
	Version              string
	TotalTracks          int
	ErrorMessage         string
}

func (c *Client) TestConnection(cfg *core.ServerConfig) ConnectionTestResult {
	res := ConnectionTestResult{}

	healthURL := fmt.Sprintf("%s/health", cfg.BaseURL())
	resp, err := c.httpClient.Get(healthURL)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("Server unreachable: %v", err)
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.ErrorMessage = fmt.Sprintf("Health check returned status %d", resp.StatusCode)
		return res
	}

	var status core.ServerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
		res.Reachable = true
		res.ServerName = status.Name
		res.Version = status.Version
		res.TotalTracks = status.TotalTracks
		for _, cap := range status.Capabilities {
			if cap == "streaming" {
				res.StreamingSupported = true
			}
			if cap == "acquisition" {
				res.AcquisitionSupported = true
			}
			if cap == "library" {
				res.LibraryAvailable = true
			}
		}
	}

	if cfg.Password != "" {
		token, err := c.Login(cfg)
		if err != nil {
			res.ErrorMessage = fmt.Sprintf("Authentication failed: %v", err)
			return res
		}
		cfg.Token = token
		res.Authenticated = true
	} else {
		res.Authenticated = true
	}

	return res
}

func (c *Client) Login(cfg *core.ServerConfig) (string, error) {
	loginURL := fmt.Sprintf("%s/api/v1/auth/login", cfg.BaseURL())
	body, _ := json.Marshal(map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	})

	resp, err := c.httpClient.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	return data.Token, nil
}

func (c *Client) BrowseLibrary(cfg *core.ServerConfig, query string) ([]core.Track, error) {
	reqURL := fmt.Sprintf("%s/api/v1/library/tracks?q=%s", cfg.BaseURL(), url.QueryEscape(query))
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server error %d", resp.StatusCode)
	}

	var tracks []core.Track
	if err := json.NewDecoder(resp.Body).Decode(&tracks); err != nil {
		return nil, err
	}

	for i := range tracks {
		tracks[i].Source = core.SourceNAS
		tracks[i].ServerID = cfg.ID
		tokenParam := ""
		if cfg.Token != "" {
			tokenParam = fmt.Sprintf("?token=%s", cfg.Token)
		}
		tracks[i].StreamURL = fmt.Sprintf("%s/api/v1/stream/%s%s", cfg.BaseURL(), tracks[i].ID, tokenParam)
	}

	return tracks, nil
}

func (c *Client) SaveToNAS(cfg *core.ServerConfig, track *core.Track) (*core.AcquisitionJob, error) {
	jobsURL := fmt.Sprintf("%s/api/v1/jobs", cfg.BaseURL())

	sourceRef := track.SourceID
	if track.StreamURL != "" && track.Source == core.SourceOnline {
		sourceRef = track.StreamURL
	}

	payload, _ := json.Marshal(map[string]string{
		"source":       sourceRef,
		"track_title":  track.Title,
		"track_artist": track.Artist,
		"album":        track.Album,
	})

	req, err := http.NewRequest(http.MethodPost, jobsURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to enqueue remote job (status %d): %s", resp.StatusCode, string(body))
	}

	var job core.AcquisitionJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, err
	}

	return &job, nil
}

func (c *Client) GetJobs(cfg *core.ServerConfig) ([]core.AcquisitionJob, error) {
	jobsURL := fmt.Sprintf("%s/api/v1/jobs", cfg.BaseURL())
	req, err := http.NewRequest(http.MethodGet, jobsURL, nil)
	if err != nil {
		return nil, err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server error %d", resp.StatusCode)
	}

	var jobs []core.AcquisitionJob
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (c *Client) CancelJob(cfg *core.ServerConfig, jobID string) error {
	reqURL := fmt.Sprintf("%s/api/v1/jobs/%s/cancel", cfg.BaseURL(), jobID)
	req, err := http.NewRequest(http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to cancel job: %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) RetryJob(cfg *core.ServerConfig, jobID string) error {
	reqURL := fmt.Sprintf("%s/api/v1/jobs/%s/retry", cfg.BaseURL(), jobID)
	req, err := http.NewRequest(http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to retry job: %d", resp.StatusCode)
	}
	return nil
}
