package wshandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/audio"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/metrics"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/tts"
)

// runPipeline reads transcripts from sess.Transcripts, debounces them, and
// dispatches exactly one goroutine per debounce window to call the LLM.
// Using a pending-slot channel avoids the goroutine-per-transcript pattern
// that previously spawned 5–8 sleeping goroutines per utterance.
// Runs until ctx is cancelled or sess.Transcripts is closed.
func runPipeline(ctx context.Context, sess *CallSession, provider *llm.Provider, store *rstore.Store) {
	// pending holds the most recent transcript waiting to be dispatched.
	// Capacity 1: new transcripts overwrite the previous one before dispatch.
	pending := make(chan string, 1)

	// Dispatcher: drains pending after a 150ms quiet window.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case transcript, ok := <-pending:
				if !ok {
					return
				}
				// Wait for the debounce window, then check if a newer
				// transcript replaced this one in the pipeline.
				ts := sess.StampTranscript()
				time.Sleep(150 * time.Millisecond)
				if sess.LastTranscript() == ts {
					go processTranscript(ctx, sess, transcript, ts, provider, store)
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case transcript, ok := <-sess.Transcripts:
			if !ok {
				return
			}
			// Non-blocking send: drop the previous pending transcript if the
			// dispatcher hasn't consumed it yet (newer utterance supersedes it).
			select {
			case pending <- transcript:
			default:
				// Drain and replace with the newer transcript.
				select {
				case <-pending:
				default:
				}
				pending <- transcript
			}
		}
	}
}

// processTranscript is the per-turn logic: takeover check → backchannel → LLM → TTS queue.
// ts is the debounce stamp set by the dispatcher in runPipeline — the dispatcher
// already waited 150ms and confirmed it's still current before calling us.
// Mirrors Python's _process_transcript in ws_handler.py.
func processTranscript(ctx context.Context, sess *CallSession, transcript string, ts int64, provider *llm.Provider, store *rstore.Store) {
	// --- Check manager takeover ---
	if store.GetTakeover(ctx, sess.StreamSid) {
		return
	}

	// --- PostTTS cooldown: wait if TTS just finished ---
	if ms := sess.MsSinceTTSEnd(); ms < 200 {
		time.Sleep(time.Duration(200-ms) * time.Millisecond)
	}

	// --- Acquire LLM lock (one turn at a time) ---
	sess.llmMu.Lock()
	defer sess.llmMu.Unlock()
	// Re-check stamp after acquiring lock: a newer transcript may have arrived
	// while this goroutine was waiting for the lock.
	if sess.LastTranscript() != ts || sess.HangupRequested() {
		return
	}

	// --- Broadcast user transcript to monitor connections ---
	sess.BroadcastTranscript("user", transcript)

	// --- Conversational Backchanneling ---
	// If user spoke >2 words, inject a filler 60% of the time so the AI
	// sounds natural while waiting for the LLM response.
	// Mirrors Python ws_handler.py Phase 2 backchanneling block.
	if len(strings.Fields(transcript)) > 2 && rand.Float64() < 0.6 {
		filler := randomFiller(sess.Language)
		select {
		case sess.TTSSentences <- filler:
		case <-ctx.Done():
			return
		}
	}

	// --- Inject whispers (manager hints) as additional context ---
	whispers, _ := store.PopAllWhispers(ctx, sess.StreamSid)
	for _, w := range whispers {
		sess.AppendHistory("user", "[Manager hint]: "+w)
	}

	// --- Record user transcript in history ---
	sess.AppendHistory("user", transcript)
	history := sess.HistorySnapshot()

	// --- Call LLM (streaming) with latency tracking ---
	responseBuilder := strings.Builder{}
	hasHangup := false
	firstChunk := true
	tPreLLM := time.Now()

	var err error
	if provider != nil {
		err = provider.ProcessTranscript(ctx, llm.TranscriptRequest{
			Transcript:   transcript,
			SystemPrompt: sess.SystemPrompt,
			History:      history[:max(0, len(history)-1)], // exclude the turn we just added
			Language:     sess.Language,
			MaxTokens:    sess.MaxTokens(),
		}, func(chunk llm.SentenceChunk) {
			if firstChunk && chunk.Text != "" {
				// Record LLM TTFB: time from transcript to first sentence chunk
				metrics.LLMFirstByteLatency.Observe(time.Since(tPreLLM).Seconds())
				firstChunk = false
			}
			if chunk.HasHangup {
				hasHangup = true
				sess.RequestHangup()
			}
			if chunk.Text != "" {
				responseBuilder.WriteString(chunk.Text)
				responseBuilder.WriteString(" ")
				select {
				case sess.TTSSentences <- chunk.Text:
				case <-ctx.Done():
				}
			}
		})
	}

	// Record total LLM round-trip latency (metric name kept for dashboard compatibility)
	metrics.GRPCLatency.Observe(time.Since(tPreLLM).Seconds())

	if err != nil && !errors.Is(err, context.Canceled) {
		sess.Log.Error("pipeline: ProcessTranscript error", zap.Error(err))
	}

	// --- Record AI response in history and broadcast to monitors ---
	if resp := strings.TrimSpace(responseBuilder.String()); resp != "" {
		sess.AppendHistory("model", resp)
		sess.BroadcastTranscript("agent", resp)
	}

	// --- Signal TTS worker that HANGUP follows the last sentence ---
	if hasHangup {
		select {
		case sess.TTSSentences <- "": // empty = hangup sentinel
		case <-ctx.Done():
		}
	}
}

// runTTSWorker reads sentences from sess.TTSSentences, calls the TTS provider,
// and sends the resulting PCM audio to the phone via the WebSocket.
// An empty sentence ("") is the HANGUP sentinel: drain + grace period + close.
//
// The provider is looked up on the session each iteration (rather than closed
// over at worker start) so that handleStartEvent can swap the instance when
// the Redis-hydrated campaign uses a different provider than the pre-loaded
// default. Without this, a call whose campaign is configured for SmallestAI
// but whose default was Sarvam would always synthesise via Sarvam.
func runTTSWorker(ctx context.Context, sess *CallSession) {
	for {
		select {
		case <-ctx.Done():
			return
		case sentence, ok := <-sess.TTSSentences:
			if !ok {
				return
			}
			if sentence == "" {
				// HANGUP sentinel: wait for remaining audio then close
				remaining := sess.PlaybackTracker.RemainingDuration()
				sess.Log.Info("hangup: waiting for playback drain",
					zap.Duration("remaining", remaining))
				waitStart := time.Now()
				select {
				case <-time.After(remaining + 7*time.Second):
				case <-ctx.Done():
				}
				metrics.HangupWait.Observe(time.Since(waitStart).Seconds())
				sess.WS.Close() //nolint:errcheck
				return
			}
			provider := sess.TTSInstance()
			if provider == nil {
				sess.Log.Warn("TTS worker: no provider available, dropping sentence",
					zap.String("sentence", sentence))
				continue
			}
			synthesizeAndSend(ctx, sess, provider, sentence)
		}
	}
}

// synthesizeAndSend calls the TTS provider for one sentence and streams
// the resulting PCM audio to the phone via the WebSocket.
func synthesizeAndSend(ctx context.Context, sess *CallSession, provider tts.Provider, sentence string) {
	ttsCtx, cancel := context.WithCancel(ctx)
	sess.SetCancelTTS(cancel)
	defer cancel()

	sess.SetTTSPlaying(true)
	defer func() {
		sess.SetTTSPlaying(false)
		sess.MarkTTSEnd()
	}()

	tPreTTS := time.Now()
	firstChunk := true

	err := provider.Synthesize(ttsCtx, sentence, sess.TTSLanguage, sess.TTSVoiceID,
		func(pcm8k []byte) {
			if firstChunk {
				metrics.TTSFirstByteLatency.Observe(time.Since(tPreTTS).Seconds())
				firstChunk = false
			}
			sendAudioFrame(sess, pcm8k)
		},
	)
	if err != nil && !errors.Is(err, context.Canceled) {
		sess.Log.Warn("TTS error", zap.String("sentence", sentence), zap.Error(err))
	}
}

// sendAudioFrame encodes PCM audio and sends it to the phone via the WebSocket.
// Handles ulaw conversion for Exotel and JSON framing differences.
func sendAudioFrame(sess *CallSession, pcm8k []byte) {
	// Record for server-side stereo WAV
	sess.AppendTTSChunk(pcm8k)
	// Feed echo canceller (ulaw representation)
	sess.EchoCanceller.FeedTTS(audio.PCMToUlaw(pcm8k))

	// Encode audio
	var audioBytes []byte
	if sess.IsExotel {
		audioBytes = audio.PCMToUlaw(pcm8k)
	} else {
		audioBytes = pcm8k
	}
	sess.PlaybackTracker.AddBytes(len(audioBytes))

	// JSON key differs between Exotel (camelCase) and web sim (snake_case)
	var frameKey string
	if sess.IsExotel {
		frameKey = "streamSid"
	} else {
		frameKey = "stream_sid"
	}

	payloadB64 := base64.StdEncoding.EncodeToString(audioBytes)
	frame, _ := json.Marshal(map[string]interface{}{
		"event":   "media",
		frameKey:  sess.StreamSid,
		"media":   map[string]string{"payload": payloadB64},
	})
	_ = sess.SendText(frame)

	// Relay a copy of the agent's outbound audio to any attached monitors so
	// external consumers can render / play back what the AI is saying.
	if sess.hasMonitors() {
		format := "pcm16_8k"
		if sess.IsExotel {
			format = "ulaw_8k"
		}
		sess.BroadcastAudio("agent", payloadB64, format)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
