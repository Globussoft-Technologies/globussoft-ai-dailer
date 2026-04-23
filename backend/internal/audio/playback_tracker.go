package audio

import (
	"sync"
	"time"
)

const (
	ulawBytesPerSec = 8000  // ulaw at 8kHz mono
	pcmBytesPerSec  = 16000 // PCM 16-bit at 8kHz mono
)

// PlaybackTracker calculates exact remaining audio playback duration
// from bytes sent — replaces Python's crude sleep(7).
type PlaybackTracker struct {
	mu          sync.Mutex
	firstByteAt time.Time
	bytesSent   int64
	bytesPerSec int64
}

func NewPlaybackTracker(isExotel bool) *PlaybackTracker {
	bps := int64(pcmBytesPerSec)
	if isExotel {
		bps = ulawBytesPerSec
	}
	return &PlaybackTracker{bytesPerSec: bps}
}

// AddBytes records n bytes sent to the phone.
func (t *PlaybackTracker) AddBytes(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstByteAt.IsZero() {
		t.firstByteAt = time.Now()
	}
	t.bytesSent += int64(n)
}

// RemainingDuration returns the estimated time until all queued audio has played.
// Adds a 500ms jitter buffer for telecom network lag.
func (t *PlaybackTracker) RemainingDuration() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstByteAt.IsZero() || t.bytesSent == 0 {
		return 500 * time.Millisecond
	}
	totalDur := time.Duration(float64(t.bytesSent) / float64(t.bytesPerSec) * float64(time.Second))
	elapsed := time.Since(t.firstByteAt)
	remaining := totalDur - elapsed + 500*time.Millisecond
	if remaining < 0 {
		return 500 * time.Millisecond
	}
	return remaining
}

// Reset clears the tracker (call at start of each TTS sentence).
func (t *PlaybackTracker) Reset() {
	t.mu.Lock()
	t.firstByteAt = time.Time{}
	t.bytesSent = 0
	t.mu.Unlock()
}
