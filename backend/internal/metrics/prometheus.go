// Package metrics provides Prometheus instrumentation for the Go audio service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ActiveCalls is the number of currently active WebSocket call sessions.
	ActiveCalls = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "callified_active_calls",
		Help: "Number of currently active WebSocket call sessions.",
	})

	// CallDuration records total call duration in seconds.
	CallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_call_duration_seconds",
		Help:    "Total call duration from WebSocket connect to close.",
		Buckets: prometheus.DefBuckets,
	})

	// STTFirstByteLatency records time from call connect to first STT transcript.
	STTFirstByteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_stt_ttfb_seconds",
		Help:    "Latency from call connect to first Deepgram transcript.",
		Buckets: prometheus.DefBuckets,
	})

	// LLMFirstByteLatency records time from user transcript to first LLM sentence chunk.
	LLMFirstByteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_llm_ttfb_seconds",
		Help:    "Latency from user transcript to first streamed LLM sentence chunk.",
		Buckets: prometheus.DefBuckets,
	})

	// TTSFirstByteLatency records time from sentence to first TTS audio chunk.
	TTSFirstByteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_tts_ttfb_seconds",
		Help:    "Latency from TTS sentence submission to first PCM audio chunk.",
		Buckets: prometheus.DefBuckets,
	})

	// GRPCLatency records the full round-trip duration of gRPC ProcessTranscript calls.
	GRPCLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_grpc_latency_seconds",
		Help:    "Full round-trip latency of Go→Python gRPC ProcessTranscript calls.",
		Buckets: prometheus.DefBuckets,
	})

	// HangupWait records the actual playback drain wait before WebSocket close.
	HangupWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "callified_hangup_wait_seconds",
		Help:    "Seconds waited for audio playback drain before HANGUP close.",
		Buckets: prometheus.DefBuckets,
	})

	// EchoSuppressions counts audio frames suppressed by the echo canceller.
	EchoSuppressions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "callified_echo_suppressions_total",
		Help: "Total number of audio frames suppressed as echo by the echo canceller.",
	})

	// BargeIns counts user interruptions of active TTS playback.
	BargeIns = promauto.NewCounter(prometheus.CounterOpts{
		Name: "callified_barge_in_total",
		Help: "Total number of user barge-in events (speech detected during TTS).",
	})
)
