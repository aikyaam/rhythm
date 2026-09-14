//go:build !windows

package graphics

type CoverWindow struct{}

var globalWindow = &CoverWindow{}

func GetCoverWindow() *CoverWindow {
	return globalWindow
}

func (cw *CoverWindow) Start() error {
	return nil
}

func (cw *CoverWindow) UpdateCover(imagePathOrURL, title, artist string) error {
	return nil
}

func (cw *CoverWindow) Toggle() bool {
	return false
}

func (cw *CoverWindow) IsVisible() bool {
	return false
}

func (cw *CoverWindow) Close() {}
