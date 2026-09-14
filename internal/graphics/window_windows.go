//go:build windows

package graphics

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

type CoverWindow struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	running bool
	visible bool
}

var (
	globalWindow   *CoverWindow
	globalWindowMu sync.Mutex
)

func GetCoverWindow() *CoverWindow {
	globalWindowMu.Lock()
	defer globalWindowMu.Unlock()
	if globalWindow == nil {
		globalWindow = &CoverWindow{}
	}
	return globalWindow
}

const coverWindowScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

[System.Windows.Forms.Application]::EnableVisualStyles()

$form = New-Object System.Windows.Forms.Form
$form.Text = "Rhythm - Cover Art"
$form.Size = New-Object System.Drawing.Size(340, 370)
$form.StartPosition = "Manual"
$form.Location = New-Object System.Drawing.Point(50, 50)
$form.BackColor = [System.Drawing.ColorTranslator]::FromHtml("#16161e")
$form.ForeColor = [System.Drawing.ColorTranslator]::FromHtml("#c0caf5")
$form.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::FixedSingle
$form.MaximizeBox = $false
$form.TopMost = $true

$pb = New-Object System.Windows.Forms.PictureBox
$pb.Dock = [System.Windows.Forms.DockStyle]::Fill
$pb.SizeMode = [System.Windows.Forms.PictureBoxSizeMode]::Zoom
$pb.BackColor = [System.Drawing.ColorTranslator]::FromHtml("#16161e")
$form.Controls.Add($pb)

$form.Add_FormClosing({
    param($sender, $e)
    $e.Cancel = $true
    $form.Hide()
})

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Write-Host "READY"

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 80
$timer.Add_Tick({
    if ([Console]::KeyAvailable -or ($reader.Peek() -ge 0)) {
        $line = $reader.ReadLine()
        if ($null -eq $line) {
            $form.Close()
            [System.Windows.Forms.Application]::Exit()
            return
        }
        $line = $line.Trim()
        if ($line -eq "") { return }

        $parts = $line.Split(" ", 2)
        $cmd = $parts[0].ToUpper()
        $arg = ""
        if ($parts.Length -gt 1) { $arg = $parts[1] }

        switch ($cmd) {
            "LOAD" {
                try {
                    if ($arg.StartsWith("http://") -or $arg.StartsWith("https://")) {
                        $pb.LoadAsync($arg)
                    } elseif ([System.IO.File]::Exists($arg)) {
                        $img = [System.Drawing.Image]::FromFile($arg)
                        $pb.Image = $img
                    }
                    if (-not $form.Visible) { $form.Show() }
                    $form.BringToFront()
                    Write-Host "OK LOAD"
                } catch {
                    Write-Host ("ERR " + $_.Exception.Message)
                }
            }
            "TITLE" {
                $form.Text = "Rhythm - " + $arg
                Write-Host "OK TITLE"
            }
            "SHOW" {
                $form.Show()
                $form.BringToFront()
                Write-Host "OK SHOW"
            }
            "HIDE" {
                $form.Hide()
                Write-Host "OK HIDE"
            }
            "QUIT" {
                $form.Close()
                [System.Windows.Forms.Application]::Exit()
            }
        }
    }
})

$reader = [Console]::In
$timer.Start()
[System.Windows.Forms.Application]::Run($form)
`

func (cw *CoverWindow) Start() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.running {
		return nil
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", coverWindowScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start cover window process: %w", err)
	}

	cw.cmd = cmd
	cw.stdin = stdin
	cw.stdout = bufio.NewReader(stdout)

	readyCh := make(chan error, 1)
	go func() {
		line, err := cw.stdout.ReadString('\n')
		if err != nil {
			readyCh <- err
			return
		}
		if strings.TrimSpace(line) == "READY" {
			readyCh <- nil
		} else {
			readyCh <- fmt.Errorf("unexpected handshake: %s", line)
		}
	}()

	cw.running = true
	cw.visible = false
	return nil
}

func (cw *CoverWindow) UpdateCover(imagePathOrURL, title, artist string) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.running {
		cw.mu.Unlock()
		if err := cw.Start(); err != nil {
			return err
		}
		cw.mu.Lock()
	}

	if title != "" {
		fmt.Fprintf(cw.stdin, "TITLE %s - %s\n", artist, title)
	}
	if imagePathOrURL != "" {
		fmt.Fprintf(cw.stdin, "LOAD %s\n", imagePathOrURL)
		cw.visible = true
	}
	return nil
}

func (cw *CoverWindow) Toggle() bool {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.running {
		cw.mu.Unlock()
		_ = cw.Start()
		cw.mu.Lock()
		fmt.Fprintln(cw.stdin, "SHOW")
		cw.visible = true
		return true
	}

	if cw.visible {
		fmt.Fprintln(cw.stdin, "HIDE")
		cw.visible = false
		return false
	} else {
		fmt.Fprintln(cw.stdin, "SHOW")
		cw.visible = true
		return true
	}
}

func (cw *CoverWindow) IsVisible() bool {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.visible && cw.running
}

func (cw *CoverWindow) Close() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.running {
		return
	}

	if cw.stdin != nil {
		fmt.Fprintln(cw.stdin, "QUIT")
		cw.stdin.Close()
	}
	if cw.cmd != nil && cw.cmd.Process != nil {
		_ = cw.cmd.Process.Kill()
	}
	cw.running = false
	cw.visible = false
}
