package llm

// ChatMessage is a single conversation turn stored in session history.
// Role is "user" for the caller or "model" for the AI agent.
type ChatMessage struct {
	Role string
	Text string
}

// SentenceChunk is one complete sentence streamed back from the LLM.
type SentenceChunk struct {
	Text      string
	HasHangup bool // LLM included [HANGUP] → end the call after this sentence
}

// TranscriptRequest carries the input for a single LLM turn.
type TranscriptRequest struct {
	Transcript   string        // current user utterance
	SystemPrompt string        // system/persona instruction
	History      []ChatMessage // prior turns (excluding current transcript)
	Language     string        // e.g. "hi", "mr", "en", "ta"
	MaxTokens    int32
}
