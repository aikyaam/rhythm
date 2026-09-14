package audio

import (
	"testing"
)

func TestNativePlayerAvailability(t *testing.T) {
	p := NewNativePlayer()
	if p == nil {
		t.Fatalf("NewNativePlayer is nil")
	}
	defer p.Close()

	if !p.IsAvailable() {
		t.Fatalf("NativePlayer is not available!")
	}
}
