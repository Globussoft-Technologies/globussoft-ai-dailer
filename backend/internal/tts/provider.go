// Package tts implements streaming Text-to-Speech providers:
// ElevenLabs (HTTP), Sarvam Bulbul v3 (WebSocket), SmallestAI (HTTP).
package tts

import (
	"context"
	"fmt"
)

// Provider is the interface every TTS implementation must satisfy.
// Synthesize streams PCM audio at 8kHz 16-bit mono, calling onChunk for each chunk.
// The caller handles conversion to ulaw and WebSocket framing.
// Returns nil on success, context.Canceled on barge-in.
type Provider interface {
	Synthesize(ctx context.Context, text, language, voiceID string, onChunk func(pcm8k []byte)) error
}

// New returns the appropriate Provider for the given provider name.
// apiKeys maps provider name → API key.
func New(provider string, apiKeys map[string]string) (Provider, error) {
	switch provider {
	case "elevenlabs":
		key := apiKeys["elevenlabs"]
		if key == "" {
			return nil, fmt.Errorf("tts: ELEVENLABS_API_KEY not set")
		}
		return NewElevenLabs(key), nil
	case "sarvam":
		key := apiKeys["sarvam"]
		if key == "" {
			return nil, fmt.Errorf("tts: SARVAM_API_KEY not set")
		}
		return NewSarvam(key), nil
	case "smallest":
		key := apiKeys["smallest"]
		if key == "" {
			return nil, fmt.Errorf("tts: SMALLEST_API_KEY not set")
		}
		return NewSmallest(key), nil
	default:
		// Unknown provider: fall back to ElevenLabs if key available
		if key := apiKeys["elevenlabs"]; key != "" {
			return NewElevenLabs(key), nil
		}
		return nil, fmt.Errorf("tts: unknown provider %q and no fallback key", provider)
	}
}
