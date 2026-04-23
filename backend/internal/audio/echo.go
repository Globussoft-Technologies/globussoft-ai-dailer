package audio

import (
	"math"
	"sync"
)

const (
	echoRingSize  = 4000 // 500ms at 8kHz ulaw (8000 bytes/s)
	echoMinEnergy = 150.0
	echoThreshold = 0.55
	echoLagMin    = 640  // 80ms
	echoLagMax    = 2400 // 300ms
	echoLagStep   = 160  // check every 20ms
)

// ulawLinear is a precomputed ulaw→linear16 lookup table.
var ulawLinear [256]int16

func init() {
	for i := range 256 {
		b := byte(i)
		sign := b & 0x80
		exp := (b >> 4) & 0x07
		mant := b & 0x0F
		sample := int16((int(mant)<<1 | 1) << (int(exp) + 2))
		sample += 33
		if sign != 0 {
			sample = -sample
		}
		ulawLinear[i] = sample
	}
}

// EchoCanceller suppresses mic frames that appear to be echoes of TTS audio.
// Uses a ring buffer of the last 500ms of TTS ulaw output and normalised
// cross-correlation to detect echo. Falls back gracefully if ring buffer is empty.
type EchoCanceller struct {
	mu  sync.Mutex
	buf [echoRingSize]byte
	pos int // next write position
	n   int // bytes written (capped at echoRingSize)
}

func NewEchoCanceller() *EchoCanceller { return &EchoCanceller{} }

// FeedTTS adds outbound TTS ulaw bytes to the ring buffer.
func (e *EchoCanceller) FeedTTS(ulaw []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, b := range ulaw {
		e.buf[e.pos] = b
		e.pos = (e.pos + 1) % echoRingSize
		if e.n < echoRingSize {
			e.n++
		}
	}
}

// IsEcho returns true when the mic ulaw frame closely correlates with
// recently played TTS audio — i.e. it is an echo of the AI's own voice.
func (e *EchoCanceller) IsEcho(frame []byte) bool {
	if len(frame) == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.n < len(frame) {
		return false
	}
	if ulawRMS(frame) < echoMinEnergy {
		return false // silence — not an echo
	}
	for lag := echoLagMin; lag <= echoLagMax && lag+len(frame) <= echoRingSize; lag += echoLagStep {
		if e.xcorr(frame, lag) > echoThreshold {
			return true
		}
	}
	return false
}

func (e *EchoCanceller) xcorr(frame []byte, lag int) float64 {
	n := len(frame)
	sumAB, sumAA, sumBB := 0.0, 0.0, 0.0
	for i := range n {
		idx := (e.pos - lag - n + i + echoRingSize*4) % echoRingSize
		a := float64(ulawLinear[frame[i]])
		b := float64(ulawLinear[e.buf[idx]])
		sumAB += a * b
		sumAA += a * a
		sumBB += b * b
	}
	denom := math.Sqrt(sumAA * sumBB)
	if denom < 1e-6 {
		return 0
	}
	return sumAB / denom
}

func ulawRMS(ulaw []byte) float64 {
	sum := 0.0
	for _, b := range ulaw {
		v := float64(ulawLinear[b])
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(ulaw)))
}
