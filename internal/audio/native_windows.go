//go:build windows

package audio

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NativePlayer struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	mu        sync.Mutex
	available bool
}

const winRTScript = `
[Windows.Media.Playback.MediaPlayer, Windows.Media, ContentType = WindowsRuntime] | Out-Null
[Windows.Media.Core.MediaSource, Windows.Media, ContentType = WindowsRuntime] | Out-Null

$mp = [Windows.Media.Playback.MediaPlayer]::new()
$mp.Volume = 0.8
$global:ended = $false
$mp.add_MediaEnded({ $global:ended = $true }) | Out-Null

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Write-Host "READY"

while ($true) {
    $line = [Console]::ReadLine()
    if ($null -eq $line) { break }
    $line = $line.Trim()
    if ($line -eq "") { continue }

    $parts = $line.Split(" ", 2)
    $cmd = $parts[0].ToUpper()
    $arg = ""
    if ($parts.Length -gt 1) { $arg = $parts[1] }

    switch ($cmd) {
        "OPEN" {
            try {
                $global:ended = $false
                $uri = [System.Uri]::new($arg)
                $source = [Windows.Media.Core.MediaSource]::CreateFromUri($uri)
                $mp.Source = $source
                $mp.Play()
                Write-Host "OK OPEN"
            } catch {
                Write-Host ("ERR " + $_.Exception.Message)
            }
        }
        "PLAY" {
            $mp.Play()
            Write-Host "OK PLAY"
        }
        "PAUSE" {
            $mp.Pause()
            Write-Host "OK PAUSE"
        }
        "STOP" {
            $mp.Pause()
            $mp.Source = $null
            $global:ended = $false
            Write-Host "OK STOP"
        }
        "SEEK" {
            try {
                $sec = [double]::Parse($arg)
                $mp.PlaybackSession.Position = [System.TimeSpan]::FromSeconds($sec)
                Write-Host "OK SEEK"
            } catch {
                Write-Host "ERR SEEK"
            }
        }
        "VOL" {
            try {
                $vol = [double]::Parse($arg) / 100.0
                $mp.Volume = $vol
                Write-Host "OK VOL"
            } catch {
                Write-Host "ERR VOL"
            }
        }
        "STATUS" {
            $pos = $mp.PlaybackSession.Position.TotalSeconds
            $dur = $mp.PlaybackSession.NaturalDuration.TotalSeconds
            $st = $mp.PlaybackSession.PlaybackState
            $isEnd = if ($global:ended) { "1" } else { "0" }
            Write-Host ("STATUS " + $st + " " + $pos + " " + $dur + " " + $isEnd)
        }
        "QUIT" {
            $mp.Pause()
            $mp.Dispose()
            Write-Host "BYE"
            exit
        }
    }
}
`

func NewNativePlayer() *NativePlayer {
	p := &NativePlayer{}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", winRTScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return p
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return p
	}

	if err := cmd.Start(); err != nil {
		return p
	}

	reader := bufio.NewReader(stdout)
	readyChan := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		readyChan <- strings.TrimSpace(line)
	}()

	select {
	case res := <-readyChan:
		if res == "READY" {
			p.cmd = cmd
			p.stdin = stdin
			p.stdout = reader
			p.available = true
		}
	case <-time.After(3 * time.Second):

	}

	return p
}

func (p *NativePlayer) IsAvailable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available
}

func (p *NativePlayer) Open(uri string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return fmt.Errorf("native player not available")
	}

	_, err := io.WriteString(p.stdin, fmt.Sprintf("OPEN %s\n", uri))
	if err != nil {
		return err
	}

	res, err := p.stdout.ReadString('\n')
	if err != nil {
		return err
	}
	res = strings.TrimSpace(res)
	if strings.HasPrefix(res, "ERR") {
		return fmt.Errorf("native player open error: %s", res)
	}

	return nil
}

func (p *NativePlayer) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return nil
	}
	_, err := io.WriteString(p.stdin, "PLAY\n")
	if err != nil {
		return err
	}
	_, _ = p.stdout.ReadString('\n')
	return nil
}

func (p *NativePlayer) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return nil
	}
	_, err := io.WriteString(p.stdin, "PAUSE\n")
	if err != nil {
		return err
	}
	_, _ = p.stdout.ReadString('\n')
	return nil
}

func (p *NativePlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return nil
	}
	_, err := io.WriteString(p.stdin, "STOP\n")
	if err != nil {
		return err
	}
	_, _ = p.stdout.ReadString('\n')
	return nil
}

func (p *NativePlayer) Seek(seconds float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return nil
	}
	_, err := io.WriteString(p.stdin, fmt.Sprintf("SEEK %.2f\n", seconds))
	if err != nil {
		return err
	}
	_, _ = p.stdout.ReadString('\n')
	return nil
}

func (p *NativePlayer) SetVolume(volume int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return nil
	}
	_, err := io.WriteString(p.stdin, fmt.Sprintf("VOL %d\n", volume))
	if err != nil {
		return err
	}
	_, _ = p.stdout.ReadString('\n')
	return nil
}

func (p *NativePlayer) Status() (state string, pos float64, dur float64, ended bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.available {
		return "None", 0, 0, false, fmt.Errorf("native player not available")
	}

	_, err = io.WriteString(p.stdin, "STATUS\n")
	if err != nil {
		return "None", 0, 0, false, err
	}

	line, err := p.stdout.ReadString('\n')
	if err != nil {
		return "None", 0, 0, false, err
	}

	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "STATUS") {
		return "None", 0, 0, false, fmt.Errorf("unexpected status: %s", line)
	}

	parts := strings.Split(line, " ")
	if len(parts) >= 4 {
		state = parts[1]
		pos, _ = strconv.ParseFloat(parts[2], 64)
		dur, _ = strconv.ParseFloat(parts[3], 64)
		if len(parts) >= 5 && parts[4] == "1" {
			ended = true
		}
		return state, pos, dur, ended, nil
	}

	return "None", 0, 0, false, nil
}

func (p *NativePlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.available {
		_, _ = io.WriteString(p.stdin, "QUIT\n")
		p.available = false
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}
