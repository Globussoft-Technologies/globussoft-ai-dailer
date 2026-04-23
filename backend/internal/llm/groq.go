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

// GroqClient calls Groq via OpenAI-compatible REST SSE streaming.
type GroqClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGroqClient(apiKey, model string) *GroqClient {
	return &GroqClient{apiKey: apiKey, model: model, http: &http.Client{}}
}

// --- request types ---

type groqRequest struct {
	Model     string        `json:"model"`
	Messages  []groqMessage `json:"messages"`
	MaxTokens int32         `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// --- response types (SSE) ---

type groqStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamTokens streams tokens from Groq (OpenAI-compatible), calling onToken for each chunk.
func (g *GroqClient) StreamTokens(ctx context.Context, req TranscriptRequest, onToken func(string)) error {
	if g.apiKey == "" {
		return fmt.Errorf("groq: GROQ_API_KEY not set")
	}

	// Build messages: system (optional) + history + current user utterance
	msgs := make([]groqMessage, 0, len(req.History)+2)
	if req.SystemPrompt != "" {
		msgs = append(msgs, groqMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, msg := range req.History {
		role := msg.Role
		if role == "model" {
			role = "assistant" // Groq uses OpenAI role names
		}
		msgs = append(msgs, groqMessage{Role: role, Content: msg.Text})
	}
	msgs = append(msgs, groqMessage{Role: "user", Content: req.Transcript})

	body := groqRequest{
		Model:     g.model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("groq: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("groq: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("groq: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("groq: status %d: %v", resp.StatusCode, errBody)
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
		var chunk groqStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				onToken(choice.Delta.Content)
			}
		}
	}
	return scanner.Err()
}
