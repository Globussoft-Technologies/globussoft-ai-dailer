package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const smallestURL = "https://waves-api.smallest.ai/api/v1/lightning/get_speech"

// SmallestProvider streams TTS via SmallestAI Lightning HTTP endpoint.
// Output is already PCM at 8kHz — no resampling needed.
type SmallestProvider struct{ apiKey string }

func NewSmallest(apiKey string) *SmallestProvider { return &SmallestProvider{apiKey: apiKey} }

func (p *SmallestProvider) Synthesize(ctx context.Context, text, language, voiceID string, onChunk func([]byte)) error {
	if voiceID == "" {
		voiceID = "emily"
	}
	body, _ := json.Marshal(map[string]interface{}{
		"text":           text,
		"voice_id":       voiceID,
		"sample_rate":    8000,
		"add_wav_header": false,
		"speed":          1.0,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, smallestURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("smallest: HTTP %d — %s", resp.StatusCode, string(b))
	}

	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			onChunk(chunk)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}
