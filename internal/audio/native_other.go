//go:build !windows

package audio

type NativePlayer struct{}

func NewNativePlayer() *NativePlayer {
	return nil
}

func (p *NativePlayer) IsAvailable() bool {
	return false
}

func (p *NativePlayer) Open(uri string) error {
	return nil
}

func (p *NativePlayer) Play() error {
	return nil
}

func (p *NativePlayer) Pause() error {
	return nil
}

func (p *NativePlayer) Stop() error {
	return nil
}

func (p *NativePlayer) Seek(seconds float64) error {
	return nil
}

func (p *NativePlayer) SetVolume(volume int) error {
	return nil
}

func (p *NativePlayer) Status() (string, float64, float64, bool, error) {
	return "None", 0, 0, false, nil
}

func (p *NativePlayer) Close() {}
