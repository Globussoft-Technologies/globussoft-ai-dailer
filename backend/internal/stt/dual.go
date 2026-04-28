package stt

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DualClient runs two parallel Deepgram WebSocket sessions on the same audio
// stream — typically one with language=multi (broad coverage) and one with
// language=hi (recovers Hindi that "multi" misclassifies as Spanish/etc.).
//
// For each utterance, results from both connections are held for up to
// MergeWindow (default 300ms). Whichever side delivers higher confidence
// wins and is dispatched once via OnTranscript. Late-arriving duplicates
// from the loser are dropped.
//
// Mirrors main-branch ws_handler.py 4aa3fa3 dual-STT merge logic.
type DualClient struct {
	primary   *Client
	secondary *Client
	log       *zap.Logger

	OnTranscript    func(text string)
	OnSpeechStarted func()

	MergeWindow time.Duration
}

// NewDualClient creates a DualClient with given API key, primary language
// (e.g. "mr" — usually mapped to multi), and secondary language ("hi").
// The secondary connection is mostly useful when primary is "multi", to
// recover Hindi misidentified as Spanish/etc.
func NewDualClient(apiKey, primaryLang, secondaryLang string, log *zap.Logger) *DualClient {
	return &DualClient{
		primary:     NewClient(apiKey, primaryLang, log),
		secondary:   NewClient(apiKey, secondaryLang, log),
		log:         log,
		MergeWindow: 300 * time.Millisecond,
	}
}

// Run streams audioIn to both Deepgram connections concurrently, merging
// their final transcripts on the way out. Blocks until ctx is cancelled or
// audioIn closes. Audio frames are fanned out — each client receives every
// frame via its own buffered channel.
func (d *DualClient) Run(ctx context.Context, audioIn <-chan []byte) {
	primaryAudio := make(chan []byte, 64)
	secondaryAudio := make(chan []byte, 64)

	merger := newSttMerger(d.MergeWindow, d.OnTranscript, d.log)

	d.primary.OnTranscriptWithConf = func(text string, conf float64) {
		merger.submit("primary", text, conf)
	}
	d.secondary.OnTranscriptWithConf = func(text string, conf float64) {
		merger.submit("secondary", text, conf)
	}
	if d.OnSpeechStarted != nil {
		d.primary.OnSpeechStarted = d.OnSpeechStarted
		// Only the primary fires SpeechStarted to the caller; the secondary's
		// barge-in signals would double-trigger TTS cancellation.
	}

	var wg sync.WaitGroup
	wg.Add(3)

	// Fanout: read audioIn once, copy to both channels.
	go func() {
		defer wg.Done()
		defer close(primaryAudio)
		defer close(secondaryAudio)
		for {
			select {
			case <-ctx.Done():
				return
			case pcm, ok := <-audioIn:
				if !ok {
					return
				}
				// Non-blocking sends — drop if either client is slow rather
				// than backpressure the phone audio path.
				select {
				case primaryAudio <- pcm:
				default:
				}
				select {
				case secondaryAudio <- pcm:
				default:
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		d.primary.Run(ctx, primaryAudio)
	}()
	go func() {
		defer wg.Done()
		d.secondary.Run(ctx, secondaryAudio)
	}()

	wg.Wait()
	merger.flush()
}

// sttMerger holds the most recent transcript from each source and, after
// MergeWindow elapses without a competing result, dispatches the winner.
type sttMerger struct {
	mu       sync.Mutex
	window   time.Duration
	pending  *pendingTranscript
	timer    *time.Timer
	deliver  func(text string)
	log      *zap.Logger
}

type pendingTranscript struct {
	source     string
	text       string
	confidence float64
}

func newSttMerger(window time.Duration, deliver func(string), log *zap.Logger) *sttMerger {
	return &sttMerger{window: window, deliver: deliver, log: log}
}

// submit registers a candidate transcript. If nothing is pending, start the
// merge window. If a competing transcript is already pending, pick the
// higher-confidence winner immediately and deliver it (cancelling the timer).
func (m *sttMerger) submit(source, text string, confidence float64) {
	if text == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pending == nil {
		m.pending = &pendingTranscript{source: source, text: text, confidence: confidence}
		m.timer = time.AfterFunc(m.window, m.flushTimer)
		return
	}

	// Two transcripts in flight — pick the higher confidence and deliver now.
	prev := m.pending
	newCand := &pendingTranscript{source: source, text: text, confidence: confidence}
	winner := prev
	loser := newCand
	if newCand.confidence > prev.confidence {
		winner, loser = newCand, prev
	}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.pending = nil
	if m.log != nil {
		m.log.Info("[STT MERGE]",
			zap.String("winner_src", winner.source),
			zap.Float64("winner_conf", winner.confidence),
			zap.String("loser_src", loser.source),
			zap.Float64("loser_conf", loser.confidence),
		)
	}
	if m.deliver != nil {
		m.deliver(winner.text)
	}
}

// flushTimer fires when the merge window expires with only one candidate.
// That candidate wins by default.
func (m *sttMerger) flushTimer() {
	m.mu.Lock()
	if m.pending == nil {
		m.mu.Unlock()
		return
	}
	winner := m.pending
	m.pending = nil
	m.timer = nil
	m.mu.Unlock()
	if m.deliver != nil {
		m.deliver(winner.text)
	}
}

// flush is called on shutdown to deliver any pending transcript.
func (m *sttMerger) flush() {
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	if pending != nil && m.deliver != nil {
		m.deliver(pending.text)
	}
}
