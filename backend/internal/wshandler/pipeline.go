package wshandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

	// Dispatcher: drains pending after a short quiet window.
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
				time.Sleep(75 * time.Millisecond)
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
// already waited briefly and confirmed it's still current before calling us.
// Mirrors Python's _process_transcript in ws_handler.py.
func processTranscript(ctx context.Context, sess *CallSession, transcript string, ts int64, provider *llm.Provider, store *rstore.Store) {
	if sess.IsFinalClosing() {
		return
	}
	// --- Voicemail detection (highest priority — runs before LLM, takeover, etc.) ---
	// If the carrier picks up with "you have reached…" / "leave a message after the
	// beep" we abandon LLM, drop a one-sentence pitch, and hang up. Mirrors
	// main-branch ws_handler.py 4aa3fa3 voicemail handling.
	if sess.HangupRequested() && (!sess.IsBargeInActive() || sess.IsFinalClosing()) {
		// Hangup was requested, but a barge-in means the customer interrupted the
		// goodbye and wants to keep talking — let the turn through.
		return
	}
	if isVoicemail(transcript) {
		handleVoicemail(ctx, sess, transcript)
		return
	}

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
	// while this goroutine was waiting for the lock. Allow the turn through if a
	// barge-in is active, even if a hangup had been requested.
	if sess.LastTranscript() != ts || (sess.HangupRequested() && (!sess.IsBargeInActive() || sess.IsFinalClosing())) {
		return
	}

	// --- Broadcast user transcript to monitor connections ---
	sess.BroadcastTranscript("user", transcript)
	sess.AppendHistory("user", transcript)

	if sess.ConsumeMaxDurationWaitReply() {
		closeLine := maxDurationClosingLineForReply(sess.Language, transcript)
		sess.RequestMaxDurationClose()
		sess.BroadcastTranscript("agent", closeLine)
		sess.AppendHistory("model", closeLine)
		select {
		case sess.TTSSentences <- closeLine:
		case <-ctx.Done():
			return
		}
		select {
		case sess.TTSSentences <- "":
		case <-ctx.Done():
		}
		return
	}

	// Turn-level control notes (barge-in guidance, repeated-question handling,
	// manager whispers) are injected into the SYSTEM instruction for this
	// request only — never into the customer's user-turn message. Notes placed
	// in the user turn are treated as conversation content and the model
	// paraphrases them aloud to the customer; system-level notes stay silent.
	var turnNotes []string
	if sess.ConsumeRecentConfirmedBargeIn(5 * time.Second) {
		turnNotes = append(turnNotes, "[Customer interrupted while the agent was speaking. If this directly answers the current question, accept it and continue. If not, address it briefly and return to the same unanswered question.]")
	}
	repeatIntent := ""
	if provider != nil {
		intentCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		if key, keyErr := provider.ClassifyRepeatIntent(intentCtx, transcript, sess.Language); keyErr == nil {
			repeatIntent = key
		} else {
			sess.Log.Debug("repeat-question: intent classification failed", zap.Error(keyErr))
		}
		cancel()
	}
	repeatDecision := sess.RepeatedQuestionDecisionWithKey(transcript, repeatIntent)
	allowHangupForTurn := repeatDecision.AllowHangup
	if instruction := repeatDecision.Instruction; instruction != "" {
		turnNotes = append(turnNotes, instruction)
	}

	// --- Manager whispers: current-turn context only, not chat history ---
	whispers, _ := store.PopAllWhispers(ctx, sess.StreamSid)
	for _, w := range whispers {
		turnNotes = append(turnNotes, "[Manager hint]: "+w)
	}

	systemPrompt := sess.SystemPrompt
	if len(turnNotes) > 0 {
		systemPrompt = sess.SystemPrompt +
			"\n\n[TURN CONTROL NOTES — internal system data. NEVER speak, translate, paraphrase, summarize, or acknowledge any of this. It is invisible to the customer. Customer-facing reply only.]\n" +
			strings.Join(turnNotes, "\n")
	}

	history := sess.HistorySnapshot()

	// --- Call LLM (streaming) with latency tracking ---
	responseBuilder := strings.Builder{}
	hasHangup := false
	firstChunk := true
	tPreLLM := time.Now()

	var err error
	if provider != nil {
		err = provider.ProcessTranscript(ctx, llm.TranscriptRequest{
			Transcript:              transcript,
			SystemPrompt:            systemPrompt,
			History:                 history[:max(0, len(history)-1)], // exclude the turn we just added
			Language:                sess.Language,
			MaxTokens:               sess.MaxTokens(transcript),
			DropIncompleteRemainder: sess.IsInbound,
		}, func(chunk llm.SentenceChunk) {
			if sess.IsFinalClosing() {
				return
			}
			if firstChunk && chunk.Text != "" {
				// Record LLM TTFB: time from transcript to first sentence chunk
				metrics.LLMFirstByteLatency.Observe(time.Since(tPreLLM).Seconds())
				firstChunk = false
				// New response starting — clear barge-in so TTS worker stops
				// discarding sentences and the agent can speak again.
				sess.SetBargeIn(false)
			}
			if chunk.HasHangup {
				if allowHangupForTurn {
					hasHangup = true
					if repeatDecision.FinalClose {
						sess.RequestFinalClose()
					} else {
						sess.RequestHangup()
					}
				} else {
					sess.Log.Warn("repeat-question: suppressed early hangup")
				}
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

type callHangupper interface {
	Hangup(ctx context.Context, callSid string, campaignID int64) error
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
func runTTSWorker(ctx context.Context, sess *CallSession, initiator callHangupper) {
	for {
		select {
		case <-ctx.Done():
			return
		case sentence, ok := <-sess.TTSSentences:
			if !ok {
				return
			}
			if sentence == "" {
				// HANGUP sentinel: wait for remaining audio then close. Abort if a
				// barge-in cancels a normal hangup before playback finishes. A
				// max-duration close is final and cannot be cancelled by late speech.
				remaining := sess.PlaybackTracker.RemainingDuration()
				sess.Log.Info("hangup: waiting for playback drain",
					zap.Duration("remaining", remaining))
				waitStart := time.Now()
				deadline := time.After(remaining + 7*time.Second)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						metrics.HangupWait.Observe(time.Since(waitStart).Seconds())
						return
					case <-deadline:
						metrics.HangupWait.Observe(time.Since(waitStart).Seconds())
						if initiator != nil && sess.CallSid != "" {
							hangupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							if err := initiator.Hangup(hangupCtx, sess.CallSid, sess.CampaignID); err != nil {
								sess.Log.Warn("hangup: carrier hangup failed",
									zap.String("call_sid", sess.CallSid),
									zap.Error(err))
							}
							cancel()
						}
						sess.WS.Close() //nolint:errcheck
						return
					case <-ticker.C:
						if !sess.HangupRequested() && !sess.IsFinalClosing() {
							metrics.HangupWait.Observe(time.Since(waitStart).Seconds())
							sess.Log.Info("hangup: aborted by barge-in")
							return
						}
					}
				}
			}
			// Safety: bridge sessions must never synthesise AI audio —
			// the agent's browser mic is the audio source, not TTS.
			if sess.IsBridge {
				sess.Log.Warn("tts worker: dropping sentence for bridge session — should not happen",
					zap.String("sentence", sentence))
				continue
			}
			// BARGE-IN DISABLED: do not discard sentences on barge-in.
			// if sess.IsBargeInActive() {
			// 	sess.Log.Info("barge-in: discarding stale sentence", zap.String("text", sentence))
			// 	continue
			// }
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
	sess.MarkTTSNewUtterance()
	defer func() {
		sess.SetTTSPlaying(false)
		sess.MarkTTSEnd()
	}()

	tPreTTS := time.Now()
	firstChunk := true

	// Debug trace so we can confirm what language each utterance was
	// synthesized in. If the user reports "AI spoke Hindi but I saved Telugu"
	// this log line shows whether sess.TTSLanguage was actually carried
	// through, vs. being lost / overridden somewhere upstream. Cheap to keep
	// in production — fires once per agent utterance, not per audio chunk.
	sess.Log.Info("tts: synthesize",
		zap.String("language", sess.TTSLanguage),
		zap.String("voice_id", sess.TTSVoiceID),
		zap.String("provider_kind", fmt.Sprintf("%T", provider)),
		zap.Int("text_len", len(sentence)),
	)

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
//
// Codec choice (μ-law vs PCM-16) is driven by sess.UseUlaw, which is set in
// handleStartEvent from the start-envelope key casing — not by sess.IsExotel.
// Voicebot real-Dial is IsExotel=true but speaks PCM-16 like web-sim.
//
// μ-law paths additionally need 20ms frame pacing. Exotel's Stream/Passthru
// applet feeds the carrier's jitter buffer, which expects 160-byte μ-law
// frames sent at wallclock pace (~20ms @ 8 kHz). Bursting the whole utterance
// in a single WS frame renders as garbled / chipmunk audio at the phone —
// the "voice not audible properly" symptom. PCM-16 paths (Voicebot, web-sim)
// don't care about pacing: the browser handles its own playback queue, and
// the Voicebot applet decodes the WS payload directly into its outbound RTP
// stream without a jitter buffer in between.
func sendAudioFrame(sess *CallSession, pcm8k []byte) {
	if sess.IsBargeInActive() {
		return
	}
	// Record for server-side stereo WAV
	sess.AppendTTSChunk(pcm8k)
	// Feed echo canceller (ulaw representation)
	sess.EchoCanceller.FeedTTS(audio.PCMToUlaw(pcm8k))

	if sess.UseUlaw {
		// μ-law: encode then slice into 20ms frames and pace.
		ulaw := audio.PCMToUlaw(pcm8k)
		sess.PlaybackTracker.AddBytes(len(ulaw))
		const frameBytes = 160 // 160 bytes µ-law = 20ms @ 8 kHz
		for off := 0; off < len(ulaw); off += frameBytes {
			end := off + frameBytes
			if end > len(ulaw) {
				end = len(ulaw)
			}
			payloadB64 := base64.StdEncoding.EncodeToString(ulaw[off:end])
			frame, _ := json.Marshal(map[string]interface{}{
				"event":     "media",
				"streamSid": sess.StreamSid,
				"media":     map[string]string{"payload": payloadB64},
			})
			_ = sess.SendText(frame)
			if sess.hasMonitors() {
				sess.BroadcastAudio("agent", payloadB64, "ulaw_8k")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return
	}

	// PCM-16 path: Voicebot / web-sim. One frame per utterance, snake_case key,
	// raw PCM payload. No pacing — the consumer queues the audio itself.
	sess.PlaybackTracker.AddBytes(len(pcm8k))
	payloadB64 := base64.StdEncoding.EncodeToString(pcm8k)
	frameData := map[string]interface{}{
		"event":      "media",
		"stream_sid": sess.StreamSid,
		"media":      map[string]string{"payload": payloadB64},
	}
	if sess.Provider == "tata" {
		seq := sess.outboundSeq.Add(1)
		frameData["streamSid"] = sess.StreamSid
		frameData["sequenceNumber"] = seq
		frameData["stream_id"] = sess.StreamSid
		frameData["stream_sid"] = sess.StreamSid
		frameData["payload"] = payloadB64
		frameData["audio"] = payloadB64
	}
	frame, _ := json.Marshal(frameData)
	_ = sess.SendText(frame)
	// Track when we last sent audio so barge-in stays armed while audio is still
	// in flight to the phone/carrier (fixes barge-in misses on long sentences).
	sess.MarkAudioSent()

	// Relay a copy of the agent's outbound audio to any attached monitors so
	// external consumers can render / play back what the AI is saying.
	if sess.hasMonitors() {
		sess.BroadcastAudio("agent", payloadB64, "pcm16_8k")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
