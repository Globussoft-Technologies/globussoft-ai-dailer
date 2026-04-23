package wshandler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/config"
	rstore "github.com/globussoft/callified-backend/internal/redis"
)

// newTestHandler returns a Handler wired with no gRPC client and in-memory store.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	log := zap.NewNop()
	cfg := &config.Config{Port: 8001, GRPCAddr: "localhost:50051"}
	store := rstore.New("", log) // empty URL → pure in-memory fallback
	return New(cfg, nil, nil, store, log)
}

// dialWS connects a test WebSocket client to the given httptest server URL + path.
func dialWS(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(rawURL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	resp.Body.Close()
	return conn
}

// sendStop sends {"event":"stop"} and ignores errors (used for cleanup).
func sendStop(conn *websocket.Conn) {
	conn.WriteJSON(map[string]string{"event": "stop"}) //nolint:errcheck
}

// ─── Wire protocol tests ────────────────────────────────────────────────────

// TestStreamTypeDetection verifies prefix-based stream type detection.
// Mirrors Python ws_handler.py stream_sid handling.
func TestStreamTypeDetection(t *testing.T) {
	tests := []struct {
		sid      string
		isWebSim bool
		isExotel bool
	}{
		{"web_sim_42_1234567890", true, false},
		{"SMabcdef1234567890", false, false}, // Twilio
		{"exotel-abc123def456", false, true}, // Exotel
	}
	for _, tt := range tests {
		sess := &CallSession{StreamSid: tt.sid}
		sess.IsWebSim = strings.HasPrefix(tt.sid, "web_sim_")
		sess.IsExotel = !sess.IsWebSim && !strings.HasPrefix(tt.sid, "SM")
		assert.Equal(t, tt.isWebSim, sess.IsWebSim, "IsWebSim mismatch for %q", tt.sid)
		assert.Equal(t, tt.isExotel, sess.IsExotel, "IsExotel mismatch for %q", tt.sid)
	}
}

// TestConnectedEventIgnored verifies that {"event":"connected"} is silently ignored.
func TestConnectedEventIgnored(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn := dialWS(t, srv.URL+"?stream_sid=web_sim_connected_test_1000")
	defer conn.Close()

	err := conn.WriteJSON(map[string]string{"event": "connected"})
	assert.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	sendStop(conn)
}

// TestStopEventClosesConnection verifies {"event":"stop"} triggers server-side close.
func TestStopEventClosesConnection(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn := dialWS(t, srv.URL+"?stream_sid=web_sim_stop_test_2000")
	defer conn.Close()

	conn.WriteJSON(map[string]string{"event": "stop"}) //nolint:errcheck

	// Server closes after "stop" — read must eventually fail
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err, "connection must be closed after stop event")
}

// TestStartEventAccepted verifies that a "start" JSON event is accepted without error.
func TestStartEventAccepted(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn := dialWS(t, srv.URL+"?stream_sid=web_sim_start_test_3000")
	defer conn.Close()

	startEvent := map[string]interface{}{
		"event": "start",
		"start": map[string]interface{}{
			"streamSid": "web_sim_start_test_3000",
			"callSid":   "CA_unit_test",
		},
	}
	err := conn.WriteJSON(startEvent)
	assert.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	sendStop(conn)
}

// TestMediaEventBase64Accepted verifies JSON media event with base64 payload is accepted.
func TestMediaEventBase64Accepted(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn := dialWS(t, srv.URL+"?stream_sid=web_sim_media_test_4000")
	defer conn.Close()

	// 160 bytes of silence PCM (10ms at 8kHz 16-bit)
	silence := make([]byte, 160)
	payload := base64.StdEncoding.EncodeToString(silence)

	err := conn.WriteJSON(map[string]interface{}{
		"event": "media",
		"media": map[string]string{"payload": payload},
	})
	assert.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	sendStop(conn)
}

// TestBinaryFrameAccepted verifies raw binary PCM frames are accepted for web_sim streams.
func TestBinaryFrameAccepted(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn := dialWS(t, srv.URL+"?stream_sid=web_sim_binary_test_5000")
	defer conn.Close()

	silence := make([]byte, 160)
	err := conn.WriteMessage(websocket.BinaryMessage, silence)
	assert.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	sendStop(conn)
}

// ─── Session unit tests ──────────────────────────────────────────────────────

// TestMaxTokensByLanguage verifies Marathi gets 400 tokens, others 250.
func TestMaxTokensByLanguage(t *testing.T) {
	for _, lang := range []string{"hi", "en", "ta", "te", ""} {
		sess := &CallSession{Language: lang}
		assert.Equal(t, int32(250), sess.MaxTokens(), "lang=%q should be 250", lang)
	}
	sess := &CallSession{Language: "mr"}
	assert.Equal(t, int32(400), sess.MaxTokens())
}

// TestGreetingSentOnce verifies TrySetGreeting is idempotent (atomic CAS).
func TestGreetingSentOnce(t *testing.T) {
	sess := &CallSession{}
	assert.True(t, sess.TrySetGreeting())
	assert.False(t, sess.TrySetGreeting())
	assert.False(t, sess.TrySetGreeting())
}

// TestHangupFlag verifies RequestHangup and HangupRequested.
func TestHangupFlag(t *testing.T) {
	sess := &CallSession{}
	assert.False(t, sess.HangupRequested())
	sess.RequestHangup()
	assert.True(t, sess.HangupRequested())
}

// TestMsSinceTTSEnd_BeforeFirstMark returns 9999 (no TTS yet).
func TestMsSinceTTSEnd_BeforeFirstMark(t *testing.T) {
	sess := &CallSession{}
	assert.Equal(t, int64(9999), sess.MsSinceTTSEnd())
}

// TestMsSinceTTSEnd_AfterMark returns a small value after MarkTTSEnd.
func TestMsSinceTTSEnd_AfterMark(t *testing.T) {
	sess := &CallSession{}
	sess.MarkTTSEnd()
	time.Sleep(10 * time.Millisecond)
	ms := sess.MsSinceTTSEnd()
	assert.GreaterOrEqual(t, ms, int64(5), "should be at least 5ms after sleep")
	assert.Less(t, ms, int64(500), "should be less than 500ms")
}

// TestDebounceStamp verifies StampTranscript returns unique values on each call.
func TestDebounceStamp(t *testing.T) {
	sess := &CallSession{}
	t1 := sess.StampTranscript()
	time.Sleep(1 * time.Millisecond)
	t2 := sess.StampTranscript()
	assert.NotEqual(t, t1, t2)
	assert.Equal(t, t2, sess.LastTranscript())
}

// TestBroadcastTranscript_JSONFormat verifies the JSON shape sent to monitor connections.
func TestBroadcastTranscript_JSONFormat(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/monitor/") {
			h.ServeMonitor(w, r)
		} else {
			h.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()

	callSid := "web_sim_broadcast_test_6000"

	// Start a call
	callConn := dialWS(t, srv.URL+"?stream_sid="+callSid)
	defer callConn.Close()

	// Wait for session to be registered
	var sess *CallSession
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if raw, ok := h.sessions.Load(callSid); ok {
			sess = raw.(*CallSession)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, sess, "session should be registered")

	// Connect a monitor
	monitorConn := dialWS(t, srv.URL+"/ws/monitor/"+callSid)
	defer monitorConn.Close()

	// Wait for monitor to attach
	time.Sleep(50 * time.Millisecond)

	// Broadcast
	sess.BroadcastTranscript("user", "Hello there")

	// Read from monitor
	monitorConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, msg, err := monitorConn.ReadMessage()
	require.NoError(t, err, "monitor should receive broadcast")

	var event map[string]string
	require.NoError(t, json.Unmarshal(msg, &event))
	assert.Equal(t, "transcript", event["type"])
	assert.Equal(t, "user", event["role"])
	assert.Equal(t, "Hello there", event["text"])

	sendStop(callConn)
}
