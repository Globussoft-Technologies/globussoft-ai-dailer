// Package stt implements a Deepgram streaming STT client using raw WebSocket.
// There is no official Go SDK; Deepgram's protocol is standard JSON-over-WebSocket.
package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// knownDeepgramIPs are fallback IPs used when DNS resolution fails.
// These are real Deepgram edge IPs observed in production; they change
// rarely but DNS lookup is the preferred path when it works.
var knownDeepgramIPs = []string{
	"208.184.56.200",
	"66.103.225.60",
	"66.103.225.59",
}

// Client connects to Deepgram's streaming STT API and forwards PCM audio.
// Callbacks are called from the receive goroutine — keep them non-blocking.
type Client struct {
	apiKey   string
	language string // e.g. "hi", "mr-IN", "en"
	model    string // "nova-2" or "nova-3"
	log      *zap.Logger

	OnTranscript         func(text string)
	OnSpeechStarted      func()
	OnTranscriptWithConf func(text string, confidence float64)
}

// NewClient creates a Deepgram STT client.
func NewClient(apiKey, language string, log *zap.Logger) *Client {
	dgLang, dgModel := mapLanguage(language)
	return &Client{
		apiKey:   apiKey,
		language: dgLang,
		model:    dgModel,
		log:      log,
	}
}

// resolveAddr resolves api.deepgram.com to a host:port string once at call
// start. All reconnects during the call reuse this address — no DNS lookup
// happens during reconnects, so DNS timeouts can never cause mid-call silence.
// Falls back to knownDeepgramIPs if DNS is unavailable.
func (c *Client) resolveAddr() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "api.deepgram.com")
	if err == nil && len(addrs) > 0 {
		c.log.Info("deepgram: resolved", zap.String("ip", addrs[0]))
		return net.JoinHostPort(addrs[0], "443")
	}
	c.log.Warn("deepgram: DNS failed, trying fallback IPs", zap.Error(err))
	// Test each fallback IP; use the first one that accepts a TCP connection.
	for _, ip := range knownDeepgramIPs {
		addr := net.JoinHostPort(ip, "443")
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			c.log.Info("deepgram: fallback IP reachable", zap.String("ip", ip))
			return addr
		}
	}
	c.log.Warn("deepgram: all fallback IPs failed, will use hostname")
	return "" // last resort: let DNS run per-connect
}

// Run connects to Deepgram and streams PCM audio from audioIn until the channel
// is closed or ctx is cancelled. Reconnects automatically on drop or every 25s.
// DNS is resolved once at start; all reconnects reuse the cached IP.
func (c *Client) Run(ctx context.Context, audioIn <-chan []byte) {
	// Resolve once — reused for every reconnect during this call.
	resolvedAddr := c.resolveAddr()

	keepalive := time.NewTicker(5 * time.Second)
	defer keepalive.Stop()

	const maxErrors = 30 // raised: transient drops should not kill STT
	errorCount := 0

	for {
		if ctx.Err() != nil {
			return
		}
		if errorCount >= maxErrors {
			c.log.Error("deepgram: too many errors, STT disabled for remainder of call",
				zap.Int("errors", errorCount))
			return
		}

		conn, err := c.connectTo(resolvedAddr)
		if err != nil {
			errorCount++
			c.log.Error("deepgram: connect failed", zap.Int("errors", errorCount), zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		// Reset error count on success so transient bursts don't accumulate.
		errorCount = 0

		connLifetime := time.NewTimer(25 * time.Second)

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
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`)) //nolint:errcheck
				<-recvDone
				conn.Close()
				connLifetime.Stop()
				return

			case <-connLifetime.C:
				c.log.Info("deepgram: proactive reconnect (25 s lifetime)")
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`)) //nolint:errcheck
				conn.Close()
				break sendLoop

			case <-recvDone:
				errorCount++
				c.log.Warn("deepgram: connection dropped, reconnecting",
					zap.Int("errors", errorCount))
				unexpectedDrop = true
				break sendLoop

			case <-keepalive.C:
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"KeepAlive"}`)) //nolint:errcheck

			case pcm, ok := <-audioIn:
				if !ok {
					conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`)) //nolint:errcheck
					<-recvDone
					conn.Close()
					connLifetime.Stop()
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, pcm); err != nil {
					errorCount++
					c.log.Warn("deepgram: send error, reconnecting",
						zap.Int("errors", errorCount), zap.Error(err))
					<-recvDone
					unexpectedDrop = true
					break sendLoop
				}
			}
		}

		connLifetime.Stop()
		if unexpectedDrop {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

// connectTo dials Deepgram using resolvedAddr (host:port) so no DNS lookup
// happens during the connection. TLS SNI still uses "api.deepgram.com" via
// the URL host, so the server certificate is validated correctly.
func (c *Client) connectTo(resolvedAddr string) (*websocket.Conn, error) {
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
	q.Set("endpointing", "200")
	q.Set("utterance_end_ms", "500")
	q.Set("interim_results", "true")
	q.Set("vad_events", "true")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("Authorization", "Token "+c.apiKey)

	tcpDialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 10 * time.Second,
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	if resolvedAddr != "" {
		// Bypass DNS: connect directly to the pre-resolved IP.
		dialer.NetDialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return tcpDialer.DialContext(ctx, network, resolvedAddr)
		}
		c.log.Info("deepgram: connecting", zap.String("addr", resolvedAddr))
	} else {
		dialer.NetDialContext = tcpDialer.DialContext
		c.log.Info("deepgram: connecting (via DNS)", zap.String("url", u.String()))
	}

	conn, resp, err := dialer.DialContext(context.Background(), u.String(), headers)
	if err != nil {
		if resp != nil {
			dgErrHdr := resp.Header.Get("dg-error")
			var body string
			if resp.Body != nil {
				b, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				body = string(b)
			}
			return nil, fmt.Errorf("deepgram dial: %w (status=%d dg-error=%q body=%q model=%s lang=%s)",
				err, resp.StatusCode, dgErrHdr, body, c.model, c.language)
		}
		return nil, fmt.Errorf("deepgram dial: %w (model=%s lang=%s addr=%s)", err, c.model, c.language, resolvedAddr)
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

type deepgramMsg struct {
	Type    string `json:"type"`
	IsFinal bool   `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

func (c *Client) handleMessage(raw []byte) {
	var msg deepgramMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	c.log.Debug("deepgram: event", zap.String("type", msg.Type))
	switch msg.Type {
	case "Results":
		if msg.IsFinal && len(msg.Channel.Alternatives) > 0 {
			alt := msg.Channel.Alternatives[0]
			if alt.Transcript == "" {
				return
			}
			if c.OnTranscript != nil {
				c.OnTranscript(alt.Transcript)
			}
			if c.OnTranscriptWithConf != nil {
				c.OnTranscriptWithConf(alt.Transcript, alt.Confidence)
			}
		}
	case "SpeechStarted":
		c.log.Info("deepgram: SpeechStarted received")
		if c.OnSpeechStarted != nil {
			c.OnSpeechStarted()
		}
	}
}

// mapLanguage converts our language code to Deepgram's language + model.
func mapLanguage(lang string) (dgLang, dgModel string) {
	switch lang {
	case "hi":
		return "hi", "nova-2"
	case "en", "":
		return "en", "nova-2"
	case "ta", "te", "kn", "ml", "gu", "bn", "pa", "mr":
		return "multi", "nova-2"
	default:
		return "multi", "nova-2"
	}
}
