package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/globussoft/callified-backend/internal/audio"
)

const (
	elBaseURL      = "https://api.elevenlabs.io/v1/text-to-speech"
	elModel        = "eleven_turbo_v2_5"
	elChunkAccum   = 1280 // accumulate 1280 bytes at 16kHz before decimating
	elChunkSleepMS = 20   // ms between PCM chunks sent to phone
)

// ElevenLabsProvider streams TTS via ElevenLabs HTTP streaming endpoint.
// Output is pcm_16000; we decimate 2:1 to produce 8kHz PCM for onChunk.
type ElevenLabsProvider struct{ apiKey string }

func NewElevenLabs(apiKey string) *ElevenLabsProvider {
	return &ElevenLabsProvider{apiKey: apiKey}
}

func (p *ElevenLabsProvider) Synthesize(ctx context.Context, text, language, voiceID string, onChunk func([]byte)) error {
	body, _ := json.Marshal(map[string]interface{}{
		"text":     text,
		"model_id": elModel,
		"language_code": language,
		"voice_settings": map[string]interface{}{
			"stability":         0.35,
			"similarity_boost":  0.85,
			"style":             0.1,
			"use_speaker_boost": true,
		},
	})

	url := fmt.Sprintf("%s/%s/stream?output_format=pcm_16000&optimize_streaming_latency=3", elBaseURL, voiceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("xi-api-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("elevenlabs: HTTP %d — %s", resp.StatusCode, string(b))
	}

	// Accumulate 1280 bytes at 16kHz, then decimate 2:1 → 640 bytes at 8kHz
	pcmBuf := make([]byte, 0, elChunkAccum*2)
	readBuf := make([]byte, 640)

	for {
		n, err := resp.Body.Read(readBuf)
		if n > 0 {
			pcmBuf = append(pcmBuf, readBuf[:n]...)
			for len(pcmBuf) >= elChunkAccum {
				chunk := pcmBuf[:elChunkAccum]
				pcmBuf = pcmBuf[elChunkAccum:]
				pcm8k := audio.Decimate2x(chunk)
				onChunk(pcm8k)
				time.Sleep(elChunkSleepMS * time.Millisecond)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Check context cancellation (barge-in)
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// Flush remaining
	if len(pcmBuf) >= 4 { // need at least 2 samples for decimation
		pcm8k := audio.Decimate2x(pcmBuf)
		if len(pcm8k) > 0 {
			onChunk(pcm8k)
		}
	}
	return nil
}
