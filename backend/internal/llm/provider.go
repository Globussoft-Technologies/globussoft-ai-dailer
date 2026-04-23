package llm

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/config"
)

// Provider routes LLM calls to Gemini or Groq and handles streaming sentence splitting.
// Language "mr" (Marathi) always uses Gemini for better Devanagari support.
// All other languages use LLM_PROVIDER env var (default: gemini).
type Provider struct {
	gemini *GeminiClient
	groq   *GroqClient
	cfg    *config.Config
	log    *zap.Logger
}

// NewProvider creates a Provider wired to Gemini and Groq from cfg.
func NewProvider(cfg *config.Config, log *zap.Logger) *Provider {
	return &Provider{
		gemini: NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel),
		groq:   NewGroqClient(cfg.GroqAPIKey, cfg.GroqModel),
		cfg:    cfg,
		log:    log,
	}
}

// ProcessTranscript calls the selected LLM, streams the response, splits it into
// sentences via SplitBuffer, detects [HANGUP], and calls onSentence for each sentence.
// Mirrors Python ws_handler.py _process_transcript LLM section.
func (p *Provider) ProcessTranscript(ctx context.Context, req TranscriptRequest, onSentence func(SentenceChunk)) error {
	var buf strings.Builder

	// Marathi always uses Gemini; all others follow LLM_PROVIDER config
	useGemini := req.Language == "mr" || p.cfg.LLMProvider != "groq"
	providerName := "groq"
	if useGemini {
		providerName = "gemini"
	}
	p.log.Info("[LLM] processing transcript",
		zap.String("provider", providerName),
		zap.String("language", req.Language),
		zap.Int32("max_tokens", req.MaxTokens),
	)

	onToken := func(token string) {
		buf.WriteString(token)
		sentences, remainder := SplitBuffer(buf.String())
		buf.Reset()
		buf.WriteString(remainder)
		for _, sent := range sentences {
			if text, hangup := parseChunk(sent); text != "" || hangup {
				onSentence(SentenceChunk{Text: text, HasHangup: hangup})
			}
		}
	}

	var err error
	if useGemini {
		err = p.gemini.StreamTokens(ctx, req, onToken)
	} else {
		err = p.groq.StreamTokens(ctx, req, onToken)
	}

	// Flush any text left in the buffer after stream ends (no trailing punctuation)
	if remaining := strings.TrimSpace(buf.String()); remaining != "" {
		text, hangup := parseChunk(remaining)
		if text != "" || hangup {
			onSentence(SentenceChunk{Text: text, HasHangup: hangup})
		}
	}

	return err
}

// GenerateResponse calls the LLM without streaming and returns the full reply.
// Used by WA agent, prompt generation, and other non-real-time contexts.
func (p *Provider) GenerateResponse(ctx context.Context, systemPrompt string, history []ChatMessage, maxTokens int32) (string, error) {
	req := TranscriptRequest{
		Transcript:   "", // will use last message from history
		SystemPrompt: systemPrompt,
		History:      history,
		MaxTokens:    maxTokens,
	}
	// Extract the last user message as the transcript
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			req.Transcript = history[i].Text
			req.History = history[:i]
			break
		}
	}

	useGemini := p.cfg.LLMProvider != "groq"
	var result strings.Builder
	onToken := func(t string) { result.WriteString(t) }

	var err error
	if useGemini {
		err = p.gemini.StreamTokens(ctx, req, onToken)
	} else {
		err = p.groq.StreamTokens(ctx, req, onToken)
	}
	return strings.TrimSpace(result.String()), err
}

// GenerateText calls Gemini (non-streaming) with thinking disabled.
// Suitable for batch extraction tasks like product scraping and prompt generation.
func (p *Provider) GenerateText(ctx context.Context, systemPrompt, userMessage string, maxOutputTokens int) (string, error) {
	return p.gemini.GenerateText(ctx, systemPrompt, userMessage, maxOutputTokens)
}

// parseChunk strips [HANGUP] from text and returns (cleanText, hasHangup).
func parseChunk(text string) (string, bool) {
	hasHangup := strings.Contains(text, "[HANGUP]")
	clean := strings.TrimSpace(strings.ReplaceAll(text, "[HANGUP]", ""))
	return clean, hasHangup
}
