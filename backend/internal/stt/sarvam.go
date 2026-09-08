package stt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const sarvamSTTURL = "https://api.sarvam.ai/speech-to-text"

// SarvamClient transcribes Indian-language audio via Sarvam's batch STT API.
// It uses energy-based VAD to detect utterance boundaries, buffers each
// utterance as a WAV, and POSTs it to Sarvam with language_code=unknown so
// Sarvam auto-detects the language (te-IN, hi-IN, en-IN, …).
//
// Latency profile: ~500 ms speech buffer + ~200 ms API round-trip.
// This is acceptable for phone conversations and much more accurate than
// Deepgram "multi" for South Indian languages.
type SarvamClient struct {
	apiKey string
	log    *zap.Logger

	OnTranscript         func(text string)
	OnSpeechStarted      func()
	OnTranscriptWithLang func(text, detectedLang string)
	// CurrentLang returns the session's active language at call time.
	// Set by the handler so transcribe() can skip re-verification when the
	// detected language already matches the current one.
	CurrentLang func() string

	// consecutivePaDetections counts back-to-back utterances confirmed as Punjabi.
	// Auto-switch to Punjabi is only allowed after 2 consecutive detections,
	// preventing single ambiguous utterances from falsely triggering a switch.
	consecutivePaDetections int
}

// NewSarvamClient creates a Sarvam STT client.
func NewSarvamClient(apiKey string, log *zap.Logger) *SarvamClient {
	return &SarvamClient{apiKey: apiKey, log: log}
}

// VAD constants for 8 kHz 16-bit mono PCM (20 ms frames = 320 bytes).
const (
	sarvamSpeechThreshold = 300_000 // mean-square energy; ~RMS 548
	sarvamSilenceFrames   = 15      // 300 ms of silence ends an utterance
	sarvamMinSpeechBytes  = 4800    // 300 ms minimum speech (avoid noise pops)
	sarvamMaxSpeechBytes  = 160000  // 10 s maximum utterance
)

// Run reads PCM from audioIn, segments it into utterances using energy VAD,
// and transcribes each utterance via the Sarvam API. Blocks until ctx is
// cancelled or audioIn is closed.
func (c *SarvamClient) Run(ctx context.Context, audioIn <-chan []byte) {
	var (
		buf          []byte
		silenceCount int
		inSpeech     bool
	)

	for {
		select {
		case <-ctx.Done():
			return
		case pcm, ok := <-audioIn:
			if !ok {
				if inSpeech && len(buf) >= sarvamMinSpeechBytes {
					c.transcribe(ctx, buf)
				}
				return
			}

			isSpeech := sarvamPCMEnergy(pcm) > sarvamSpeechThreshold

			if isSpeech {
				if !inSpeech {
					inSpeech = true
					silenceCount = 0
					if c.OnSpeechStarted != nil {
						c.OnSpeechStarted()
					}
				}
				silenceCount = 0
				buf = append(buf, pcm...)

				// Hard cap: flush before buffer gets too large.
				if len(buf) >= sarvamMaxSpeechBytes {
					c.transcribe(ctx, buf)
					buf = nil
					inSpeech = false
				}
			} else if inSpeech {
				silenceCount++
				buf = append(buf, pcm...) // include trailing silence for natural cut
				if silenceCount >= sarvamSilenceFrames {
					if len(buf) >= sarvamMinSpeechBytes {
						c.transcribe(ctx, buf)
					}
					buf = nil
					inSpeech = false
					silenceCount = 0
				}
			}
		}
	}
}

type sarvamResponse struct {
	Transcript   string `json:"transcript"`
	LanguageCode string `json:"language_code"`
}

// transcribeRaw sends a pre-built WAV to Sarvam STT and returns the raw response.
func (c *SarvamClient) transcribeRaw(ctx context.Context, wavData []byte) (sarvamResponse, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return sarvamResponse{}, err
	}
	if _, err := fw.Write(wavData); err != nil {
		return sarvamResponse{}, err
	}
	_ = w.WriteField("model", "saarika:v2.5")
	_ = w.WriteField("language_code", "unknown")
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sarvamSTTURL, &body)
	if err != nil {
		return sarvamResponse{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Api-Subscription-Key", c.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sarvamResponse{}, err
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		c.log.Error("sarvam stt: non-200",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBytes)))
		return sarvamResponse{}, nil
	}

	var result sarvamResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return sarvamResponse{}, err
	}
	return result, nil
}

// transcribe sends one utterance (PCM) to Sarvam STT and fires callbacks.
// When the detected language differs from the session's current language,
// the same audio is sent a second time to verify. The language switch is
// only forwarded if both API calls agree on the same language — preventing
// single-utterance hallucinations (Sarvam is non-deterministic).
func (c *SarvamClient) transcribe(ctx context.Context, pcm []byte) {
	tStart := time.Now()
	wavData := sarvamBuildWAV(pcm)

	result, err := c.transcribeRaw(ctx, wavData)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Error("sarvam stt: http error", zap.Error(err))
		}
		return
	}

	text := strings.TrimSpace(result.Transcript)
	if text == "" {
		return
	}

	detectedLang := sarvamNormLang(result.LanguageCode)
	c.log.Info("sarvam stt: transcript",
		zap.String("text", text),
		zap.String("lang", detectedLang),
		zap.Duration("latency", time.Since(tStart)),
	)

	// Language switch must be applied BEFORE firing OnTranscript so the pipeline
	// picks up the transcript with the already-updated language. If we fire
	// OnTranscript first, the pipeline's 150ms debounce fires before the
	// re-verification completes (~300ms), causing the LLM to run in the old language.
	if c.OnTranscriptWithLang != nil {
		verifiedLang := detectedLang

		// Re-verify when this looks like a language switch: non-trivial transcript,
		// not Odia (persistent false positive), and language differs from current.
		currentLang := ""
		if c.CurrentLang != nil {
			currentLang = c.CurrentLang()
		}
		if detectedLang != "" && detectedLang != "od" &&
			len(strings.Fields(text)) >= 3 &&
			detectedLang != currentLang {

			result2, err2 := c.transcribeRaw(ctx, wavData)
			if err2 == nil {
				lang2 := sarvamNormLang(result2.LanguageCode)
				if lang2 != detectedLang {
					c.log.Info("sarvam stt: lang verify mismatch, suppressing switch",
						zap.String("lang1", detectedLang),
						zap.String("lang2", lang2),
						zap.String("text", text),
					)
					verifiedLang = "" // suppress language switch
				} else {
					c.log.Info("sarvam stt: lang verify confirmed",
						zap.String("lang", detectedLang),
						zap.String("text", text),
					)
				}
			}
		}

		// Consecutive Punjabi guard: require 2 back-to-back confirmed Punjabi
		// utterances before auto-switching any language → pa. This prevents
		// single ambiguous utterances (e.g. "pandra toh bees lakh" in a Hindi
		// or English session) from falsely triggering a Punjabi switch.
		if verifiedLang == "pa" {
			c.consecutivePaDetections++
			if c.consecutivePaDetections < 2 {
				c.log.Info("sarvam stt: punjabi consecutive guard, suppressing switch",
					zap.Int("count", c.consecutivePaDetections),
					zap.String("text", text),
				)
				verifiedLang = ""
			} else {
				c.log.Info("sarvam stt: punjabi consecutive confirmed, allowing switch",
					zap.String("text", text),
				)
				c.consecutivePaDetections = 0
			}
		} else {
			c.consecutivePaDetections = 0
		}

		c.OnTranscriptWithLang(text, verifiedLang)
	}

	// Fire pipeline transcript AFTER language switch so the LLM sees the
	// updated language when it picks up the transcript from the channel.
	if c.OnTranscript != nil {
		c.OnTranscript(text)
	}
}

// sarvamPCMEnergy returns mean-square energy of a PCM16LE byte slice.
func sarvamPCMEnergy(pcm []byte) int64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum int64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int64(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		sum += s * s
	}
	return sum / int64(n)
}

// sarvamBuildWAV wraps raw PCM 16-bit 8 kHz mono in a WAV container.
func sarvamBuildWAV(pcm []byte) []byte {
	var buf bytes.Buffer
	dataLen := uint32(len(pcm))
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }

	buf.WriteString("RIFF")
	write(uint32(36 + dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	write(uint32(16))       // subchunk size
	write(uint16(1))        // PCM
	write(uint16(1))        // channels
	write(uint32(8000))     // sample rate
	write(uint32(8000 * 2)) // byte rate
	write(uint16(2))        // block align
	write(uint16(16))       // bits per sample
	buf.WriteString("data")
	write(dataLen)
	buf.Write(pcm)
	return buf.Bytes()
}

// sarvamNormLang converts Sarvam lang codes (te-IN, hi-IN, en-IN) to our
// internal codes (te, hi, en).
func sarvamNormLang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if idx := strings.Index(code, "-"); idx > 0 {
		return code[:idx]
	}
	if code == "unknown" || code == "" {
		return ""
	}
	return code
}

// SarvamLangSupported returns true for languages where Sarvam STT is
// preferred over Deepgram (all Indian languages + English).
func SarvamLangSupported(lang string) bool {
	switch lang {
	case "hi", "te", "ta", "kn", "ml", "bn", "gu", "pa", "mr", "en", "":
		return true
	}
	return false
}
