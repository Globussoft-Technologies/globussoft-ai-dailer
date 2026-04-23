package audio

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStereoWAV_Header(t *testing.T) {
	// Build a WAV with minimal PCM data
	mic := []TimedChunk{{Ts: time.Now(), Data: make([]byte, 160)}} // 10ms of silence at 8kHz 16-bit
	tts := []TimedChunk{{Ts: time.Now(), Data: make([]byte, 160)}}

	wav := BuildStereoWAV(mic, tts)
	require.NotNil(t, wav)
	require.GreaterOrEqual(t, len(wav), 44, "WAV must be at least 44 bytes (header)")

	// Verify RIFF magic
	assert.Equal(t, []byte("RIFF"), wav[0:4])

	// Verify WAVE format
	assert.Equal(t, []byte("WAVE"), wav[8:12])

	// Verify fmt chunk
	assert.Equal(t, []byte("fmt "), wav[12:16])

	// fmt chunk size should be 16 for PCM
	fmtSize := binary.LittleEndian.Uint32(wav[16:20])
	assert.Equal(t, uint32(16), fmtSize)

	// Audio format: 1 = PCM
	audioFmt := binary.LittleEndian.Uint16(wav[20:22])
	assert.Equal(t, uint16(1), audioFmt)

	// Channels: 2 (stereo)
	channels := binary.LittleEndian.Uint16(wav[22:24])
	assert.Equal(t, uint16(2), channels)

	// Sample rate: 8000
	sampleRate := binary.LittleEndian.Uint32(wav[24:28])
	assert.Equal(t, uint32(8000), sampleRate)

	// Bits per sample: 16
	bitsPerSample := binary.LittleEndian.Uint16(wav[34:36])
	assert.Equal(t, uint16(16), bitsPerSample)

	// data chunk marker
	assert.Equal(t, []byte("data"), wav[36:40])
}

func TestBuildStereoWAV_NilInputs(t *testing.T) {
	// Both nil — should return nil (nothing to record)
	result := BuildStereoWAV(nil, nil)
	assert.Nil(t, result)
}

func TestBuildStereoWAV_EmptySlices(t *testing.T) {
	result := BuildStereoWAV([]TimedChunk{}, []TimedChunk{})
	assert.Nil(t, result, "empty chunk slices should produce nil output")
}
