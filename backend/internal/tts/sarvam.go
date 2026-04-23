package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

const sarvamWSURL = "wss://api.sarvam.ai/text-to-speech/ws?model=bulbul:v3&send_completion_event=true"

// sarvamLangCode maps our language codes to Sarvam's BCP-47 codes.
var sarvamLangCode = map[string]string{
	"hi": "hi-IN", "mr": "mr-IN", "ta": "ta-IN", "te": "te-IN",
	"bn": "bn-IN", "gu": "gu-IN", "kn": "kn-IN", "ml": "ml-IN",
	"pa": "pa-IN", "en": "en-IN",
}

// SarvamProvider streams TTS via Sarvam Bulbul v3 WebSocket.
// Output is linear16 PCM at 8kHz — no resampling needed.
type SarvamProvider struct{ apiKey string }

func NewSarvam(apiKey string) *SarvamProvider { return &SarvamProvider{apiKey: apiKey} }

func (p *SarvamProvider) Synthesize(ctx context.Context, text, language, voiceID string, onChunk func([]byte)) error {
	headers := http.Header{}
	headers.Set("Api-Subscription-Key", p.apiKey)

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, sarvamWSURL, headers)
	if err != nil {
		return fmt.Errorf("sarvam: dial: %w", err)
	}
	defer conn.Close()

	langCode := sarvamLangCode[language]
	if langCode == "" {
		langCode = "hi-IN"
	}
	if voiceID == "" {
		voiceID = "aditya"
	}

	// 1. Send config
	configMsg, _ := json.Marshal(map[string]interface{}{
		"type": "config",
		"data": map[string]interface{}{
			"model":                "bulbul:v3",
			"target_language_code": langCode,
			"speaker":              voiceID,
			"pace":                 1.0,
			"speech_sample_rate":   "8000",
			"output_audio_codec":   "linear16",
			"enable_preprocessing": true,
			"min_buffer_size":      30,
		},
	})
	if err := conn.WriteMessage(websocket.TextMessage, configMsg); err != nil {
		return fmt.Errorf("sarvam: send config: %w", err)
	}

	// 2. Send text
	textMsg, _ := json.Marshal(map[string]interface{}{
		"type": "text",
		"data": map[string]string{"text": text},
	})
	if err := conn.WriteMessage(websocket.TextMessage, textMsg); err != nil {
		return fmt.Errorf("sarvam: send text: %w", err)
	}

	// 3. Send flush
	flushMsg, _ := json.Marshal(map[string]string{"type": "flush"})
	if err := conn.WriteMessage(websocket.TextMessage, flushMsg); err != nil {
		return fmt.Errorf("sarvam: send flush: %w", err)
	}

	// 4. Receive audio until "event.final"
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil // connection closed = done
		}

		var frame map[string]interface{}
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}

		frameType, _ := frame["type"].(string)
		switch frameType {
		case "audio":
			data, _ := frame["data"].(map[string]interface{})
			if data == nil {
				continue
			}
			encoded, _ := data["audio"].(string)
			pcm, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(pcm) == 0 {
				continue
			}
			onChunk(pcm) // already linear16 at 8kHz

		case "event":
			data, _ := frame["data"].(map[string]interface{})
			if eventType, _ := data["event_type"].(string); eventType == "final" {
				return nil
			}

		case "error":
			data, _ := frame["data"].(map[string]interface{})
			msg, _ := data["message"].(string)
			return fmt.Errorf("sarvam: TTS error: %s", msg)
		}
	}
}
