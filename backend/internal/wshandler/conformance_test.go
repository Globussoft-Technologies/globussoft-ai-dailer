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
	"github.com/globussoft/callified-backend/internal/db"
	rstore "github.com/globussoft/callified-backend/internal/redis"
)

// newTestHandler returns a Handler wired with no gRPC client and in-memory store.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	log := zap.NewNop()
	cfg := &config.Config{Port: 8001, GRPCAddr: "localhost:50051"}
	store := rstore.New("", log) // empty URL → pure in-memory fallback
	return New(cfg, nil, nil, store, (*db.DB)(nil), log)
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

// TestMaxTokens verifies token allocation is based on transcript length and
// language. Non-English languages use a higher multiplier and cap.
func TestMaxTokens(t *testing.T) {
	sess := &CallSession{Language: "hi"}

	// Short transcript is clamped to the non-English minimum.
	assert.Equal(t, int32(320), sess.MaxTokens("test transcript"), "short non-English transcript should be 320")

	// Medium transcript is still clamped to the minimum.
	assert.Equal(t, int32(320), sess.MaxTokens("one two three four five six seven eight nine ten"))

	// Long transcript scales up without allowing long monologues.
	longText := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty twentyone"
	assert.Equal(t, int32(504), sess.MaxTokens(longText), "long non-English transcript should be 504")

	// English keeps a lower cap than Indian languages for faster outbound turns.
	sess.Language = "en"
	assert.Equal(t, int32(250), sess.MaxTokens("test transcript"), "short English transcript should be 250")
	assert.Equal(t, int32(420), sess.MaxTokens(longText), "long English transcript should be 420")
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

func TestMaxDurationCloseCannotBeCancelledByBargeIn(t *testing.T) {
	sess := NewCallSession("test_stream", nil, zap.NewNop())
	sess.SetBargeInPending(true)
	sess.SetBargeIn(true)
	sess.RequestMaxDurationClose()

	assert.True(t, sess.ConfirmBargeIn())
	assert.True(t, sess.HangupRequested())
	assert.True(t, sess.IsMaxDurationClosing())
	assert.False(t, sess.IsBargeInActive())
}

func TestFinalCloseCannotBeCancelledByBargeIn(t *testing.T) {
	sess := NewCallSession("test_stream", nil, zap.NewNop())
	sess.SetBargeInPending(true)
	sess.SetBargeIn(true)
	sess.RequestFinalClose()

	assert.False(t, sess.ConfirmBargeIn())
	assert.True(t, sess.HangupRequested())
	assert.True(t, sess.IsFinalClosing())
	assert.False(t, sess.IsBargeInActive())
	assert.False(t, sess.IsBargeInPending())
	assert.False(t, sess.TryBargeIn("unit-test"))
}

func TestTentativeBargeInDoesNotCancelTTSUntilConfirmed(t *testing.T) {
	sess := NewCallSession("test_stream", nil, zap.NewNop())
	cancelled := false
	sess.SetCancelTTS(func() { cancelled = true })

	assert.True(t, sess.TentativeTriggerBargeIn())
	assert.True(t, sess.IsBargeInPending())
	assert.False(t, sess.IsBargeInActive())
	assert.False(t, cancelled)

	assert.True(t, sess.ConfirmBargeIn())
	assert.True(t, sess.IsBargeInActive())
	assert.True(t, cancelled)
}

func TestMaxDurationWaitsForOneCustomerReply(t *testing.T) {
	sess := NewCallSession("test_stream", nil, zap.NewNop())

	sess.RequestMaxDurationWaitReply()

	assert.True(t, sess.IsMaxDurationSoftClosing())
	assert.True(t, sess.ConsumeMaxDurationWaitReply())
	assert.False(t, sess.ConsumeMaxDurationWaitReply())
}

func TestMaxDurationClosingLineAdaptsToFinalReply(t *testing.T) {
	provide := maxDurationClosingLineForReply("en", "It's been 100 employees, can you provide?")
	assert.Contains(t, provide, "Yes, we can help with that.")
	assert.NotContains(t, provide, "?")

	question := maxDurationClosingLineForReply("en", "Okay tell me what varies for corporate and other sections?")
	assert.Contains(t, question, "employee count")
	assert.Contains(t, question, "access points")
	assert.NotContains(t, question, "?")

	answer := maxDurationClosingLineForReply("en", "Education")
	assert.Contains(t, answer, "Got it, thank you for sharing.")
	assert.NotContains(t, answer, "?")
}

func TestFillerSoundsDoNotCountAsSpeech(t *testing.T) {
	for _, text := range []string{
		"hmm", "mm-hmm", "Mm-hmm.", "mhm", "uhh", "ఉమ్.", ".", "...", "?",
	} {
		assert.True(t, isFillerSound(text), text)
		assert.True(t, isKnownFiller(text), text)
	}
	assert.False(t, isFillerSound("hello"))
	assert.False(t, isFillerSound("హలో."))
	assert.False(t, isFillerSound("okay"))
	assert.False(t, isFillerSound("haan"))
	assert.False(t, isFillerSound("no"))
	assert.False(t, isFillerSound("ok"))
	assert.False(t, isFillerSound("okay tell me more"))
	assert.False(t, isFillerSound("hello, who is speaking"))
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
