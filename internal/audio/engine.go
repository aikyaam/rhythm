package audio

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/wav"
)

type Engine struct {
	mu           sync.RWMutex
	state        core.PlaybackState
	currentTrack *core.Track
	volume       int
	volumeEffect *effects.Volume
	ctrl         *beep.Ctrl
	streamer     beep.StreamSeekCloser
	sampleRate   beep.SampleRate
	format       beep.Format
	closer       io.Closer

	position   time.Duration
	duration   time.Duration
	posTicker  *time.Ticker
	stopTicker chan struct{}

	onTrackEnd    func()
	onStateChange func(core.PlaybackState)

	speakerInited bool
	headlessMode  bool

	native      *NativePlayer
	usingNative bool
}

func NewEngine() *Engine {
	sampleRate := beep.SampleRate(44100)
	err := speaker.Init(sampleRate, sampleRate.N(time.Second/10))
	inited := (err == nil)

	native := NewNativePlayer()

	e := &Engine{
		state:         core.StateIdle,
		volume:        80,
		sampleRate:    sampleRate,
		speakerInited: inited,
		headlessMode:  !inited && (native == nil || !native.IsAvailable()),
		native:        native,
		stopTicker:    make(chan struct{}),
	}

	return e
}

func (e *Engine) SetCallbacks(onEnd func(), onState func(core.PlaybackState)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onTrackEnd = onEnd
	e.onStateChange = onState
}

func (e *Engine) Play(track *core.Track) error {
	e.Stop()

	e.mu.Lock()
	e.currentTrack = track
	e.position = 0
	e.usingNative = false
	e.mu.Unlock()

	if track.StreamURL != "" && e.native != nil && e.native.IsAvailable() {
		err := e.native.Open(track.StreamURL)
		if err == nil {
			e.mu.Lock()
			e.usingNative = true
			e.state = core.StatePlaying
			if track.Duration > 0 {
				e.duration = time.Duration(track.Duration * float64(time.Second))
			}
			e.mu.Unlock()
			_ = e.native.SetVolume(e.Volume())
			_ = e.native.Play()
			e.startPositionTicker()
			if e.onStateChange != nil {
				e.onStateChange(core.StatePlaying)
			}
			return nil
		}
	}

	var readCloser io.ReadCloser
	var fileExt string

	if track.LocalPath != "" {
		fileExt = strings.ToLower(filepath.Ext(track.LocalPath))

		if (fileExt == ".m4a" || fileExt == ".mp4" || fileExt == ".aac" || fileExt == ".webm" || fileExt == ".opus") &&
			e.native != nil && e.native.IsAvailable() {
			err := e.native.Open(track.LocalPath)
			if err == nil {
				e.mu.Lock()
				e.usingNative = true
				e.state = core.StatePlaying
				if track.Duration > 0 {
					e.duration = time.Duration(track.Duration * float64(time.Second))
				}
				e.mu.Unlock()
				_ = e.native.SetVolume(e.Volume())
				_ = e.native.Play()
				e.startPositionTicker()
				if e.onStateChange != nil {
					e.onStateChange(core.StatePlaying)
				}
				return nil
			}
		}

		f, err := os.Open(track.LocalPath)
		if err != nil {
			return fmt.Errorf("failed to open local file: %w", err)
		}
		readCloser = f
	} else if track.StreamURL != "" {
		resp, err := http.Get(track.StreamURL)
		if err != nil {
			return fmt.Errorf("failed to stream from URL: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return fmt.Errorf("stream returned status %d", resp.StatusCode)
		}
		readCloser = resp.Body
		fileExt = ".mp3"
		if strings.Contains(track.StreamURL, ".flac") {
			fileExt = ".flac"
		} else if strings.Contains(track.StreamURL, ".wav") {
			fileExt = ".wav"
		}
	} else {
		return fmt.Errorf("track has no playable local path or stream URL")
	}

	if e.headlessMode {

		e.mu.Lock()
		e.closer = readCloser
		e.state = core.StatePlaying
		if track.Duration > 0 {
			e.duration = time.Duration(track.Duration * float64(time.Second))
		} else {
			e.duration = 3 * time.Minute
		}
		e.mu.Unlock()
		e.startPositionTicker()
		if e.onStateChange != nil {
			e.onStateChange(core.StatePlaying)
		}
		return nil
	}

	var streamer beep.StreamSeekCloser
	var format beep.Format
	var err error

	switch fileExt {
	case ".mp3":
		streamer, format, err = mp3.Decode(readCloser)
	case ".wav":
		streamer, format, err = wav.Decode(readCloser)
	case ".flac":
		streamer, format, err = flac.Decode(readCloser)
	default:

		streamer, format, err = mp3.Decode(readCloser)
	}

	if err != nil {
		readCloser.Close()

		if e.native != nil && e.native.IsAvailable() {
			target := track.LocalPath
			if target == "" {
				target = track.StreamURL
			}
			if target != "" && e.native.Open(target) == nil {
				e.mu.Lock()
				e.usingNative = true
				e.state = core.StatePlaying
				if track.Duration > 0 {
					e.duration = time.Duration(track.Duration * float64(time.Second))
				}
				e.mu.Unlock()
				_ = e.native.SetVolume(e.Volume())
				_ = e.native.Play()
				e.startPositionTicker()
				if e.onStateChange != nil {
					e.onStateChange(core.StatePlaying)
				}
				return nil
			}
		}

		e.mu.Lock()
		e.state = core.StatePlaying
		e.duration = time.Duration(track.Duration * float64(time.Second))
		e.mu.Unlock()
		e.startPositionTicker()
		return nil
	}

	resampled := beep.Resample(4, format.SampleRate, e.sampleRate, streamer)

	e.mu.Lock()
	e.closer = readCloser
	e.streamer = streamer
	e.format = format
	e.duration = format.SampleRate.D(streamer.Len())

	e.ctrl = &beep.Ctrl{Streamer: resampled, Paused: false}
	e.volumeEffect = &effects.Volume{
		Streamer: e.ctrl,
		Base:     2,
		Volume:   e.calcVolumeGain(e.volume),
		Silent:   e.volume == 0,
	}

	done := make(chan struct{})
	trackEndCallback := e.onTrackEnd

	speaker.Play(beep.Seq(e.volumeEffect, beep.Callback(func() {
		close(done)
		if trackEndCallback != nil {
			go trackEndCallback()
		}
	})))

	e.state = core.StatePlaying
	e.mu.Unlock()

	e.startPositionTicker()

	if e.onStateChange != nil {
		e.onStateChange(core.StatePlaying)
	}

	return nil
}

func (e *Engine) calcVolumeGain(vol int) float64 {
	if vol <= 0 {
		return -10.0
	}
	fraction := float64(vol) / 100.0
	return (math.Log10(fraction) * 3.0)
}

func (e *Engine) startPositionTicker() {
	e.mu.Lock()
	if e.posTicker != nil {
		e.posTicker.Stop()
	}
	e.posTicker = time.NewTicker(250 * time.Millisecond)
	ticker := e.posTicker
	e.mu.Unlock()

	go func() {
		for {
			select {
			case <-e.stopTicker:
				return
			case <-ticker.C:
				e.mu.Lock()
				if e.state == core.StatePlaying {
					if e.usingNative && e.native != nil {
						st, pos, dur, ended, err := e.native.Status()
						if err == nil {
							if pos > 0 {
								e.position = time.Duration(pos * float64(time.Second))
							}
							if dur > 0 {
								e.duration = time.Duration(dur * float64(time.Second))
							}
							if ended || (dur > 0 && pos >= dur-0.5 && st != "Playing") {
								e.mu.Unlock()
								if e.onTrackEnd != nil {
									e.onTrackEnd()
								}
								return
							}
						}
					} else if e.streamer != nil {
						speaker.Lock()
						pos := e.format.SampleRate.D(e.streamer.Position())
						speaker.Unlock()
						e.position = pos
					} else {
						e.position += 250 * time.Millisecond
						if e.duration > 0 && e.position >= e.duration {
							e.mu.Unlock()
							if e.onTrackEnd != nil {
								e.onTrackEnd()
							}
							return
						}
					}
				}
				e.mu.Unlock()
			}
		}
	}()
}

func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == core.StatePlaying {
		if e.usingNative && e.native != nil {
			_ = e.native.Pause()
		} else if e.ctrl != nil {
			speaker.Lock()
			e.ctrl.Paused = true
			speaker.Unlock()
		}
		e.state = core.StatePaused
		if e.onStateChange != nil {
			go e.onStateChange(core.StatePaused)
		}
	}
}

func (e *Engine) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == core.StatePaused {
		if e.usingNative && e.native != nil {
			_ = e.native.Play()
		} else if e.ctrl != nil {
			speaker.Lock()
			e.ctrl.Paused = false
			speaker.Unlock()
		}
		e.state = core.StatePlaying
		if e.onStateChange != nil {
			go e.onStateChange(core.StatePlaying)
		}
	}
}

func (e *Engine) TogglePlayPause() {
	e.mu.RLock()
	s := e.state
	e.mu.RUnlock()

	if s == core.StatePlaying {
		e.Pause()
	} else if s == core.StatePaused {
		e.Resume()
	}
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.usingNative && e.native != nil {
		_ = e.native.Stop()
		e.usingNative = false
	}

	if e.ctrl != nil {
		speaker.Lock()
		e.ctrl.Paused = true
		speaker.Unlock()
		speaker.Clear()
	}

	if e.closer != nil {
		e.closer.Close()
		e.closer = nil
	}

	e.streamer = nil
	e.ctrl = nil
	e.volumeEffect = nil
	e.state = core.StateStopped
	e.position = 0

	select {
	case e.stopTicker <- struct{}{}:
	default:
	}

	if e.onStateChange != nil {
		go e.onStateChange(core.StateStopped)
	}
}

func (e *Engine) Seek(deltaSeconds float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.usingNative && e.native != nil {
		newSec := e.position.Seconds() + deltaSeconds
		if newSec < 0 {
			newSec = 0
		}
		if e.duration > 0 && newSec > e.duration.Seconds() {
			newSec = e.duration.Seconds()
		}
		_ = e.native.Seek(newSec)
		e.position = time.Duration(newSec * float64(time.Second))
		return
	}

	if e.streamer == nil {
		newPos := e.position + time.Duration(deltaSeconds*float64(time.Second))
		if newPos < 0 {
			newPos = 0
		}
		if e.duration > 0 && newPos > e.duration {
			newPos = e.duration
		}
		e.position = newPos
		return
	}

	speaker.Lock()
	defer speaker.Unlock()

	currentPos := e.streamer.Position()
	sampleDelta := e.format.SampleRate.N(time.Duration(deltaSeconds * float64(time.Second)))
	newSample := currentPos + sampleDelta
	if newSample < 0 {
		newSample = 0
	}
	if newSample > e.streamer.Len() {
		newSample = e.streamer.Len()
	}

	_ = e.streamer.Seek(newSample)
	e.position = e.format.SampleRate.D(newSample)
}

func (e *Engine) SetVolume(vol int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	e.volume = vol

	if e.usingNative && e.native != nil {
		_ = e.native.SetVolume(vol)
	}

	if e.volumeEffect != nil {
		speaker.Lock()
		e.volumeEffect.Volume = e.calcVolumeGain(vol)
		e.volumeEffect.Silent = (vol == 0)
		speaker.Unlock()
	}
}

func (e *Engine) AdjustVolume(delta int) int {
	e.mu.RLock()
	current := e.volume
	e.mu.RUnlock()

	newVol := current + delta
	e.SetVolume(newVol)
	return e.Volume()
}

func (e *Engine) Volume() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.volume
}

func (e *Engine) State() core.PlaybackState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

func (e *Engine) CurrentTrack() *core.Track {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentTrack
}

func (e *Engine) Progress() (positionSeconds, durationSeconds float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	pos := e.position.Seconds()
	dur := e.duration.Seconds()
	if dur == 0 && e.currentTrack != nil {
		dur = e.currentTrack.Duration
	}
	return pos, dur
}

func (e *Engine) NativeAvailable() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.native != nil && e.native.IsAvailable()
}

func (e *Engine) Close() {
	e.Stop()
	if e.native != nil {
		e.native.Close()
	}
}
