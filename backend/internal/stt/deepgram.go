// Package stt implements a Deepgram streaming STT client using raw WebSocket.
// There is no official Go SDK; Deepgram's protocol is standard JSON-over-WebSocket.
package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Client connects to Deepgram's streaming STT API and forwards PCM audio.
// Callbacks are called from the receive goroutine — keep them non-blocking.
type Client struct {
	apiKey      string
	language    string // e.g. "hi", "mr-IN", "en"
	model       string // "nova-2" or "nova-3"
	log         *zap.Logger

	OnTranscript   func(text string)
	OnSpeechStarted func()
}

// NewClient creates a Deepgram STT client.
// language should be the language code (e.g. "hi", "mr").
// model is derived automatically: "nova-3" for Marathi, "nova-2" for all others.
func NewClient(apiKey, language string, log *zap.Logger) *Client {
	dgLang, dgModel := mapLanguage(language)
	return &Client{
		apiKey:   apiKey,
		language: dgLang,
		model:    dgModel,
		log:      log,
	}
}

// Run connects to Deepgram and streams PCM audio from audioIn until the channel
// is closed or ctx is cancelled. Transcripts are delivered via OnTranscript callback.
func (c *Client) Run(ctx context.Context, audioIn <-chan []byte) {
	conn, err := c.connect()
	if err != nil {
		c.log.Error("deepgram: connect failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Receive goroutine
	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		c.receiveLoop(conn)
	}()

	// Send audio
	for {
		select {
		case <-ctx.Done():
			// Graceful close
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`)) //nolint:errcheck
			<-recvDone
			return
		case pcm, ok := <-audioIn:
			if !ok {
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`)) //nolint:errcheck
				<-recvDone
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, pcm); err != nil {
				c.log.Warn("deepgram: send error", zap.Error(err))
				return
			}
		}
	}
}

// RunKeepalive sends a KeepAlive message every 5 seconds while ctx is active.
// Must be started as a separate goroutine.
func (c *Client) RunKeepalive(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"KeepAlive"}`)); err != nil {
				return
			}
		}
	}
}

func (c *Client) connect() (*websocket.Conn, error) {
	u := url.URL{
		Scheme: "wss",
		Host:   "api.deepgram.com",
		Path:   "/v1/listen",
	}
	q := u.Query()
	q.Set("model", c.model)
	q.Set("language", c.language)
	q.Set("encoding", "linear16")
	q.Set("sample_rate", "8000")
	q.Set("channels", "1")
	q.Set("endpointing", "300")
	q.Set("utterance_end_ms", "1000")
	q.Set("interim_results", "true")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("Authorization", "Token "+c.apiKey)

	conn, _, err := websocket.DefaultDialer.DialContext(context.Background(), u.String(), headers)
	if err != nil {
		return nil, fmt.Errorf("deepgram dial: %w", err)
	}
	return conn, nil
}

func (c *Client) receiveLoop(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		c.handleMessage(msg)
	}
}

// deepgramMsg covers all event types from Deepgram.
type deepgramMsg struct {
	Type    string `json:"type"`
	IsFinal bool   `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
		} `json:"alternatives"`
	} `json:"channel"`
}

func (c *Client) handleMessage(raw []byte) {
	var msg deepgramMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	switch msg.Type {
	case "Results":
		if msg.IsFinal && len(msg.Channel.Alternatives) > 0 {
			text := msg.Channel.Alternatives[0].Transcript
			if text != "" && c.OnTranscript != nil {
				c.OnTranscript(text)
			}
		}
	case "SpeechStarted":
		if c.OnSpeechStarted != nil {
			c.OnSpeechStarted()
		}
	}
}

// mapLanguage converts our language code to Deepgram's language + model selection.
func mapLanguage(lang string) (dgLang, dgModel string) {
	switch lang {
	case "mr":
		return "mr-IN", "nova-3"
	case "hi":
		return "hi", "nova-2"
	case "ta":
		return "ta-IN", "nova-2"
	case "te":
		return "te-IN", "nova-2"
	case "bn":
		return "bn-IN", "nova-2"
	case "gu":
		return "gu-IN", "nova-2"
	case "kn":
		return "kn-IN", "nova-2"
	case "ml":
		return "ml-IN", "nova-2"
	case "pa":
		return "pa-IN", "nova-2"
	default:
		if lang == "" {
			return "en", "nova-2"
		}
		return lang, "nova-2"
	}
}
