# Callified WebSocket Demo

Single-file HTML reference implementation. Demonstrates how an external app
talks to Callified's audio + transcript WebSocket end to end.

## Run it

The demo is pure HTML/JS — no build step, no dependencies. Pick whichever
suits the team:

```sh
# 1. Just open the file (works for everything except the SSE feed; some
#    browsers block fetch() from file:// origins with mixed http content).
open backend/examples/websocket-demo/index.html

# 2. Serve over a tiny local HTTP server so SSE / fetch behave normally.
cd backend/examples/websocket-demo
python3 -m http.server 8080
# then open http://localhost:8080
```

## What it shows

| Step | Pattern in the code |
|------|---------------------|
| 1. Connect to `wss://…/media-stream?…` with lead query params | `connect()` |
| 2. Send `{event:"connected"}` then `{event:"start", stream_sid, start:{streamSid}}` | `ws.onopen` |
| 3. Render incoming `{type:"transcript", role, text}` frames as a chat log | `ws.onmessage` `frame.type === 'transcript'` |
| 4. Decode incoming `{event:"media", media:{payload}}` (base64 PCM16 8 kHz), schedule for playback | `ws.onmessage` `frame.event === 'media'` |
| 5. Capture the mic at 48 kHz, downsample to 8 kHz PCM16, base64, send as `{event:"media"}` frames | `startMicStreaming()` |
| 6. Subscribe to `/api/campaign-events` SSE for `DIALING / CONNECTED / COMPLETED` (using the ticket flow, not raw token-in-URL) | `subscribeToLiveFeed()` |
| 7. Send `{event:"stop"}` on disconnect so the server runs `finalizeCall` | `disconnect()` |

Search the HTML for `// 1`, `// 2`, … `// 7` to jump straight to each step.

## Configuration to fill in via the UI

| Field | Notes |
|-------|-------|
| Base URL | `https://testgo.callified.ai` for prod, `http://localhost:8001` for local |
| Auth token | Optional — only needed for the live-feed (SSE) panel. Get one from `POST /api/auth/login` or grab `authToken` from your browser's localStorage on a logged-in tab. |
| Lead name / phone / lead_id | Sent as WS query params; backend uses these to render `Name (Phone)` in live-feed events |
| Campaign ID | Required for the live-feed events to fire (server skips emit when `campaign_id` is 0) |
| TTS provider | `smallest` / `sarvam` / `elevenlabs` — must match a key configured on the server |
| Language | Code passed to STT and TTS — make sure the campaign / lead audio actually matches |

## Key things to take away when adapting to your stack

### Frame shapes you handle on the wire

```ts
// Inbound transcript (text only)
type TranscriptFrame = {
  type: "transcript";
  role: "agent" | "user";
  text: string;
};

// Inbound audio chunk (Exotel/Twilio media-streams shape)
type MediaFrame = {
  event: "media";
  stream_sid: string;
  media: { payload: string }; // base64 — PCM16 8 kHz from web-sim, μ-law 8 kHz from Exotel
};

// Inbound barge-in: discard any buffered TTS audio
type ClearFrame = { event: "clear"; stream_sid: string };

// Outbound handshake
type ConnectedOut = { event: "connected" };
type StartOut = {
  event: "start";
  stream_sid: string;
  start: { streamSid: string; callSid?: string };
};

// Outbound user audio (PCM16 8 kHz base64)
type MediaOut = { event: "media"; media: { payload: string } };

// Outbound clean shutdown
type StopOut = { event: "stop"; stream_sid: string };
```

### Echo prevention

The mic captures whatever the speakers play, so without guardrails the AI
ends up "hearing" itself and going into a feedback loop. The demo mutes
the mic for the duration of incoming TTS playback plus a 500 ms tail. In
production with headphones this isn't needed, but for laptop speakers it is.

### SSE auth: ticket, not bearer

The legacy pattern `?token=<bearer>` is gone — bearers leaked into Cloudflare
logs / browser history. The new flow:

1. `GET /api/sse/ticket` with the regular `Authorization: Bearer …` → returns
   `{ "ticket": "…", "expires_in": 60 }`.
2. Use `?ticket=<ticket>` on `/api/campaign-events` and `/api/sse/live-logs`.
3. Ticket dies after 60 s, so even if it leaks it's already useless.

### What the demo does NOT cover

- **Reconnect / retry logic** — production code should re-dial on
  `ws.onerror` / unexpected `ws.onclose` with exponential backoff.
- **AudioWorklet** — `ScriptProcessorNode` is deprecated and runs on the
  main thread. AudioWorklet is the modern path; the encode logic stays
  identical, only the wiring changes.
- **Recording upload** — when the WS closes, the server saves a stereo
  WAV. If you also want the higher-quality browser webm, also POST it to
  `/api/upload-recording` with `lead_id` + the recorded blob. See
  `frontend/src/contexts/CallContext.jsx` in this repo for an example.
- **Mobile / Safari quirks** — `getUserMedia` requires a user gesture
  (button click) before the first call, which the demo respects.
