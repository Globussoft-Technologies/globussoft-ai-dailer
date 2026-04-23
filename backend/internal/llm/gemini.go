package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GeminiClient calls Google Gemini via REST SSE streaming.
type GeminiClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{apiKey: apiKey, model: model, http: &http.Client{}}
}

// --- request types ---

type geminiRequest struct {
	SystemInstruction *geminiContent   `json:"system_instruction,omitempty"`
	Contents          []geminiContent  `json:"contents"`
	GenerationConfig  map[string]int32 `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// --- response types (SSE) ---

type geminiStreamEvent struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// geminiTextRequest is used for non-streaming generateContent calls.
// Supports thinkingConfig to disable reasoning for faster, complete responses.
type geminiTextRequest struct {
	SystemInstruction *geminiContent        `json:"system_instruction,omitempty"`
	Contents          []geminiContent       `json:"contents"`
	GenerationConfig  geminiTextGenConfig   `json:"generationConfig"`
}

type geminiTextGenConfig struct {
	MaxOutputTokens int                    `json:"maxOutputTokens"`
	ThinkingConfig  *geminiThinkingConfig  `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiTextResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// GenerateText calls Gemini using the non-streaming REST endpoint with thinking disabled.
// Use this for batch/extract tasks (scraping, prompt generation) where streaming is not needed.
func (g *GeminiClient) GenerateText(ctx context.Context, systemPrompt, userMessage string, maxOutputTokens int) (string, error) {
	if g.apiKey == "" {
		return "", fmt.Errorf("gemini: GEMINI_API_KEY not set")
	}

	body := geminiTextRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userMessage}}},
		},
		GenerationConfig: geminiTextGenConfig{
			MaxOutputTokens: maxOutputTokens,
			ThinkingConfig:  &geminiThinkingConfig{ThinkingBudget: 0}, // disable thinking — not needed for extraction
		},
	}
	if systemPrompt != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: systemPrompt}}}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("gemini: marshal: %w", err)
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.model, g.apiKey,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini: http: %w", err)
	}
	defer resp.Body.Close()

	var result geminiTextResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("gemini: decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("gemini: api error %d: %s", result.Error.Code, result.Error.Message)
	}
	var sb strings.Builder
	for _, cand := range result.Candidates {
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// StreamTokens streams tokens from Gemini, calling onToken for each text chunk.
// Uses SSE endpoint: streamGenerateContent?alt=sse
func (g *GeminiClient) StreamTokens(ctx context.Context, req TranscriptRequest, onToken func(string)) error {
	if g.apiKey == "" {
		return fmt.Errorf("gemini: GEMINI_API_KEY not set")
	}

	// Build contents: history + current user utterance
	contents := make([]geminiContent, 0, len(req.History)+1)
	for _, msg := range req.History {
		contents = append(contents, geminiContent{
			Role:  msg.Role, // "user" or "model"
			Parts: []geminiPart{{Text: msg.Text}},
		})
	}
	contents = append(contents, geminiContent{
		Role:  "user",
		Parts: []geminiPart{{Text: req.Transcript}},
	})

	body := geminiRequest{
		Contents:         contents,
		GenerationConfig: map[string]int32{"maxOutputTokens": req.MaxTokens},
	}
	if req.SystemPrompt != "" {
		body.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.SystemPrompt}},
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("gemini: marshal: %w", err)
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?key=%s&alt=sse",
		g.model, g.apiKey,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gemini: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("gemini: status %d: %v", resp.StatusCode, errBody)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}
		var event geminiStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue // skip malformed chunk
		}
		if event.Error != nil {
			return fmt.Errorf("gemini: api error: %s", event.Error.Message)
		}
		for _, cand := range event.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					onToken(part.Text)
				}
			}
		}
	}
	return scanner.Err()
}
