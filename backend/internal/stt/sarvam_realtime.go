package stt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const sarvamRealtimeURL = "wss://api.sarvam.ai/speech-to-text-realtime/ws"

// SarvamRealtimeClient streams audio to Sarvam's saaras:v3-realtime WebSocket
// endpoint and receives partial + final transcripts. It is intended to replace
// the batch SarvamClient for voice-agent use because partial transcripts let
// barge-in be confirmed within the first few hundred milliseconds of speech
// instead of waiting for a full utterance to be POSTed as a WAV file.
type SarvamRealtimeClient struct {
	apiKey string
	log    *zap.Logger

	// OnSpeechStarted fires on the server's vad.speech_start event.
	OnSpeechStarted func()
	// OnPartialTranscript fires on every transcript.partial event. The text
	// may be incomplete; use it only for low-latency barge-in confirmation.
	OnPartialTranscript func(text string)
	// OnTranscript fires on transcript.final with the completed utterance.
	OnTranscript func(text string)
	// OnTranscriptWithLang fires on transcript.final with the detected language
	// code (normalized to two-letter, e.g. "hi", "ta"). Only set when the
	// server returns a language field (language_code=auto).
	OnTranscriptWithLang func(text, detectedLang string)
}

// NewSarvamRealtimeClient creates a streaming Sarvam STT client.
func NewSarvamRealtimeClient(apiKey string, log *zap.Logger) *SarvamRealtimeClient {
	return &SarvamRealtimeClient{apiKey: apiKey, log: log}
}

// Run connects to Sarvam realtime STT and streams PCM audio from audioIn until
// the channel is closed or ctx is cancelled. Reconnects automatically on
// transient drops.
func (c *SarvamRealtimeClient) Run(ctx context.Context, audioIn <-chan []byte) {
	keepalive := time.NewTicker(5 * time.Second)
	defer keepalive.Stop()

	const maxErrors = 30
	errorCount := 0

	for {
		if ctx.Err() != nil {
			return
		}
		if errorCount >= maxErrors {
			c.log.Error("sarvam realtime: too many errors, STT disabled",
				zap.Int("errors", errorCount))
			return
		}

		conn, err := c.connect()
		if err != nil {
			errorCount++
			c.log.Error("sarvam realtime: connect failed",
				zap.Int("errors", errorCount), zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		errorCount = 0

		recvDone := make(chan struct{})
		go func() {
			defer close(recvDone)
			c.receiveLoop(conn)
		}()

		unexpectedDrop := false
	sendLoop:
		for {
			select {
			case <-ctx.Done():
				c.finishConnection(conn, recvDone)
				return

			case <-recvDone:
				errorCount++
				c.log.Warn("sarvam realtime: connection dropped, reconnecting",
					zap.Int("errors", errorCount))
				unexpectedDrop = true
				break sendLoop

			case <-keepalive.C:
				c.sendJSON(conn, map[string]string{"event": "ping"})

			case pcm, ok := <-audioIn:
				if !ok {
					c.finishConnection(conn, recvDone)
					return
				}
				msg := map[string]string{
					"event": "audio_input",
					"audio": base64.StdEncoding.EncodeToString(pcm),
				}
				if err := c.sendJSON(conn, msg); err != nil {
					errorCount++
					c.log.Warn("sarvam realtime: send error, reconnecting",
						zap.Int("errors", errorCount), zap.Error(err))
					<-recvDone
					unexpectedDrop = true
					break sendLoop
				}
			}
		}

		if unexpectedDrop {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func (c *SarvamRealtimeClient) finishConnection(conn *websocket.Conn, recvDone <-chan struct{}) {
	_ = c.sendJSON(conn, map[string]string{"event": "end"})
	select {
	case <-recvDone:
	case <-time.After(1500 * time.Millisecond):
		_ = conn.Close()
		<-recvDone
	}
}

func (c *SarvamRealtimeClient) sendJSON(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, b)
}

func (c *SarvamRealtimeClient) connect() (*websocket.Conn, error) {
	u, err := url.Parse(sarvamRealtimeURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("language_code", "auto")
	q.Set("model", "saaras:v3-realtime")
	q.Set("stream_type", "fast")
	q.Set("endpointing", "vad")
	q.Set("encoding", "linear16")
	q.Set("sample_rate", "8000")
	q.Set("threshold", "0.3")
	q.Set("silence_duration_ms", "350")
	q.Set("min_speech_duration_ms", "250")
	q.Set("prefix_padding_ms", "300")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("Api-Subscription-Key", c.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 10 * time.Second,
		}).DialContext,
	}

	c.log.Info("sarvam realtime: connecting", zap.String("url", u.String()))
	conn, resp, err := dialer.DialContext(context.Background(), u.String(), headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("sarvam realtime dial: %w (status=%d body=%q)",
				err, resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("sarvam realtime dial: %w", err)
	}
	return conn, nil
}

func (c *SarvamRealtimeClient) receiveLoop(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		c.handleMessage(msg)
	}
}

type sarvamRealtimeMsg struct {
	Event      string  `json:"event"`
	Text       string  `json:"text"`
	Language   string  `json:"language"`
	Confidence float64 `json:"language_confidence"`
	Code       string  `json:"code"`
	Message    string  `json:"message"`
	IsFatal    bool    `json:"is_fatal"`
}

func (c *SarvamRealtimeClient) handleMessage(raw []byte) {
	var msg sarvamRealtimeMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		c.log.Debug("sarvam realtime: unparseable message", zap.String("raw", string(raw)), zap.Error(err))
		return
	}

	c.log.Debug("sarvam realtime: event", zap.String("event", msg.Event))

	switch msg.Event {
	case "session.begin":
		c.log.Info("sarvam realtime: session begin")

	case "vad.speech_start":
		if c.OnSpeechStarted != nil {
			c.OnSpeechStarted()
		}

	case "transcript.partial":
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		if c.OnPartialTranscript != nil {
			c.OnPartialTranscript(text)
		}

	case "transcript.final":
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		lang := sarvamNormLang(msg.Language)
		c.log.Info("sarvam realtime: final transcript",
			zap.String("text", text),
			zap.String("lang", lang),
		)
		if c.OnTranscript != nil {
			c.OnTranscript(text)
		}
		if c.OnTranscriptWithLang != nil {
			c.OnTranscriptWithLang(text, lang)
		}

	case "error":
		c.log.Error("sarvam realtime: server error",
			zap.String("code", msg.Code),
			zap.String("message", msg.Message),
			zap.Bool("is_fatal", msg.IsFatal))

	case "pong", "config.updated", "session.end", "vad.speech_end":
		// No action needed.
	}
}
