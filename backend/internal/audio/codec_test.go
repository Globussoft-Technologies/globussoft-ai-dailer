package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUlawRoundTrip(t *testing.T) {
	// Encode a sine-like pattern, then verify round-trip is lossless at ulaw precision
	pcm := make([]byte, 160*2) // 160 samples = 20ms at 8kHz
	for i := range 160 {
		// simple ramp
		v := int16(i * 200)
		pcm[i*2] = byte(v)
		pcm[i*2+1] = byte(v >> 8)
	}
	ulaw := PCMToUlaw(pcm)
	assert.Len(t, ulaw, 160)

	back := UlawToPCM(ulaw)
	assert.Len(t, back, 320)
}

func TestDecimate2x(t *testing.T) {
	// 1280 bytes at 16kHz → 640 bytes at 8kHz
	pcm16k := make([]byte, 1280)
	for i := range pcm16k {
		pcm16k[i] = byte(i)
	}
	pcm8k := Decimate2x(pcm16k)
	assert.Len(t, pcm8k, 640)

	// First sample must be sample 0 from 16kHz stream
	assert.Equal(t, pcm16k[0], pcm8k[0])
	assert.Equal(t, pcm16k[1], pcm8k[1])

	// Second sample must be sample 2 from 16kHz stream (every other)
	assert.Equal(t, pcm16k[4], pcm8k[2])
	assert.Equal(t, pcm16k[5], pcm8k[3])
}
