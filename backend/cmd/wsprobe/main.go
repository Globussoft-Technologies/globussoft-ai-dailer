// wsprobe — interactive smoke test for the wshandler WebSocket endpoints.
//
// What it does:
//
//  1. Dials ws://<host>/media-stream with browser-style query params
//     (name, phone, lead_id, campaign_id, tts_*).
//  2. Sends the same handshake the browser sends:
//        {"event":"connected"}
//        {"event":"start","start":{"streamSid":"<sid>"},"stream_sid":"<sid>"}
//  3. Reads frames for --duration and prints a one-line summary per frame:
//        text    → role/event from the JSON envelope
//        binary  → byte length (raw TTS audio)
//
// Use this to verify the live-feed events fire (DIALING/CONNECTED/COMPLETED),
// the greeting TTS streams back, and the Redis pending-call hydration works
// for new sessions. It will NOT exercise STT (no real mic frames sent).
//
// Run from inside the docker network so localhost:8001 is reachable:
//
//	docker run --rm \
//	  -v "$(pwd)/backend":/app -w /app \
//	  --network callified_default \
//	  golang:1.25-alpine \
//	  go run ./cmd/wsprobe \
//	    -url ws://callified-go-audio:8001/media-stream \
//	    -campaign 7 -name "WS Probe" -phone 9999900001 -lead 1 -duration 10s
//
// Or from the host (if go is installed and you can reach :8001 directly):
//
//	go run ./cmd/wsprobe -url ws://127.0.0.1:8001/media-stream -campaign 7 -duration 8s
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	target := flag.String("url", "ws://127.0.0.1:8001/media-stream", "ws:// endpoint to probe")
	name := flag.String("name", "WS Probe", "lead name (sent as ?name=)")
	phone := flag.String("phone", "9999900001", "lead phone (sent as ?phone=)")
	leadID := flag.Int64("lead", 0, "lead_id (sent as ?lead_id=)")
	campaign := flag.Int64("campaign", 0, "campaign_id (sent as ?campaign_id=) — required for live-feed events")
	ttsProvider := flag.String("tts", "smallest", "tts provider")
	voice := flag.String("voice", "", "tts voice id (optional)")
	lang := flag.String("lang", "en", "tts language")
	duration := flag.Duration("duration", 8*time.Second, "how long to listen before closing")
	flag.Parse()

	// Build the query string the same way the browser does.
	q := url.Values{}
	q.Set("name", *name)
	q.Set("phone", *phone)
	if *leadID > 0 {
		q.Set("lead_id", strconv.FormatInt(*leadID, 10))
	}
	if *campaign > 0 {
		q.Set("campaign_id", strconv.FormatInt(*campaign, 10))
	}
	q.Set("tts_provider", *ttsProvider)
	q.Set("tts_language", *lang)
	if *voice != "" {
		q.Set("voice", *voice)
	}

	u, err := url.Parse(*target)
	if err != nil {
		log.Fatalf("bad -url: %v", err)
	}
	u.RawQuery = q.Encode()

	fmt.Printf("→ dialing %s\n", u.String())
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			log.Fatalf("dial failed: %v (HTTP %d)", err, resp.StatusCode)
		}
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	fmt.Println("✓ connected")

	streamSid := fmt.Sprintf("web_sim_probe_%d", time.Now().UnixMilli())

	send := func(label string, payload any) {
		buf, _ := json.Marshal(payload)
		if err := conn.WriteMessage(websocket.TextMessage, buf); err != nil {
			log.Fatalf("send %s: %v", label, err)
		}
		fmt.Printf("→ %-9s %s\n", label, buf)
	}
	send("connected", map[string]any{"event": "connected"})
	send("start", map[string]any{
		"event":      "start",
		"stream_sid": streamSid,
		"start":      map[string]any{"streamSid": streamSid},
	})

	// Read frames until duration elapses or Ctrl-C.
	deadline := time.Now().Add(*duration)
	conn.SetReadDeadline(deadline)
	ctrlC := make(chan os.Signal, 1)
	signal.Notify(ctrlC, os.Interrupt)

	textCount, binCount := 0, 0
	for {
		select {
		case <-ctrlC:
			fmt.Println("\n→ interrupted, closing")
			goto done
		default:
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			if time.Now().After(deadline) {
				fmt.Println("→ deadline reached, closing")
				goto done
			}
			fmt.Printf("✗ read: %v\n", err)
			goto done
		}
		switch mt {
		case websocket.TextMessage:
			textCount++
			summary := summarizeText(data)
			fmt.Printf("← text     %s\n", summary)
		case websocket.BinaryMessage:
			binCount++
			fmt.Printf("← binary   %d bytes (audio)\n", len(data))
		}
	}
done:
	// Send a clean stop so finalizeCall fires (emits COMPLETED).
	stop := map[string]any{"event": "stop", "stream_sid": streamSid}
	if buf, _ := json.Marshal(stop); buf != nil {
		_ = conn.WriteMessage(websocket.TextMessage, buf)
		fmt.Printf("→ stop      %s\n", buf)
	}
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second))
	fmt.Printf("\nsummary: %d text frames, %d binary frames\n", textCount, binCount)
}

// summarizeText pulls a short label out of an inbound JSON envelope so the
// console output stays scannable. Falls back to the first 80 raw chars when
// the frame isn't JSON or doesn't carry an `event` / `role` field.
func summarizeText(buf []byte) string {
	var env struct {
		Event   string `json:"event"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Text    string `json:"text"`
		Format  string `json:"format"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(buf, &env); err == nil {
		switch {
		case env.Type == "transcript" && env.Text != "":
			return fmt.Sprintf("transcript[%s]: %q", env.Role, truncate(env.Text, 60))
		case env.Type == "audio":
			return fmt.Sprintf("audio[%s] format=%s payload=%d bytes",
				env.Role, env.Format, len(env.Payload))
		case env.Event != "":
			return "event=" + env.Event
		}
	}
	return truncate(string(buf), 80)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
