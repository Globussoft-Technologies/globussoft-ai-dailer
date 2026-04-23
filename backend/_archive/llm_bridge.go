// Package llm provides the LLM provider (Gemini/Groq) and the legacy gRPC bridge.
// Bridge is kept during migration (Phase 0-3) for InitializeCall / FinalizeCall gRPC.
// It is removed entirely in Phase 4 once those calls are ported to Go.
package llm

import (
	"context"
	"io"

	pb "github.com/globussoft/callified-backend/internal/pb"
)

// Bridge wraps the gRPC CallLogicServiceClient.
// Deprecated: ProcessTranscript replaced by Provider.ProcessTranscript in Phase 0.
// Kept as dead-code placeholder until Phase 4 removes all gRPC.
type Bridge struct {
	client pb.CallLogicServiceClient
}

func NewBridge(client pb.CallLogicServiceClient) *Bridge {
	return &Bridge{client: client}
}

// ProcessTranscript calls the Python gRPC ProcessTranscript RPC (legacy path).
// onSentence uses the shared SentenceChunk type so callers need no pb import.
func (b *Bridge) ProcessTranscript(ctx context.Context, req TranscriptRequest, onSentence func(SentenceChunk)) error {
	if b.client == nil {
		return nil
	}
	pbHistory := make([]*pb.ChatMessage, 0, len(req.History))
	for _, m := range req.History {
		pbHistory = append(pbHistory, &pb.ChatMessage{Role: m.Role, Text: m.Text})
	}
	stream, err := b.client.ProcessTranscript(ctx, &pb.TranscriptRequest{
		Transcript:   req.Transcript,
		SystemPrompt: req.SystemPrompt,
		History:      pbHistory,
		Language:     req.Language,
		MaxTokens:    req.MaxTokens,
	})
	if err != nil {
		return err
	}
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		onSentence(SentenceChunk{Text: chunk.Text, HasHangup: chunk.HasHangup})
	}
}
