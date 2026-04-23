package wa

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/rag"
)

const humanTakeover = "[HUMAN_TAKEOVER]"
const pauseKey = "wa:pause:" // Redis key prefix for pause flag

// Agent processes inbound WhatsApp messages and generates AI replies.
type Agent struct {
	db       *db.DB
	llm      *llm.Provider
	ragClient *rag.Client
	log      *zap.Logger
}

// NewAgent creates a WA Agent.
func NewAgent(database *db.DB, llmProvider *llm.Provider, ragClient *rag.Client, log *zap.Logger) *Agent {
	return &Agent{
		db:        database,
		llm:       llmProvider,
		ragClient: ragClient,
		log:       log,
	}
}

// ProcessIncoming handles one inbound WA message and returns the reply text.
// Returns ("", nil) if no reply should be sent (e.g. human takeover active).
func (a *Agent) ProcessIncoming(ctx context.Context, cfg ChannelConfig, msg *IncomingMessage) (string, error) {
	// 1. Dedup: skip if we already processed this provider_msg_id
	if msg.ProviderMsgID != "" {
		existing, _ := a.db.GetWAMessageByProviderID(msg.ProviderMsgID)
		if existing != nil {
			return "", nil
		}
	}

	// 2. Get or create conversation
	convID, err := a.db.GetOrCreateWAConversation(0, msg.FromPhone, msg.Provider)
	if err != nil {
		return "", fmt.Errorf("GetOrCreateWAConversation: %w", err)
	}

	// 3. Save inbound message
	if _, err := a.db.SaveWAMessage(convID, "inbound", msg.Text, msg.MessageType, msg.ProviderMsgID); err != nil {
		a.log.Warn("wa agent: SaveWAMessage inbound", zap.Error(err))
	}

	if msg.Text == "" {
		return "", nil // media-only message, no AI response
	}

	// 4. Get recent chat history (last 10 messages)
	history, _ := a.db.GetWAChatHistory(convID, 10)
	var chatHistory []llm.ChatMessage
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		role := "user"
		if h.Direction == "outbound" {
			role = "assistant"
		}
		chatHistory = append(chatHistory, llm.ChatMessage{Role: role, Text: h.MessageText})
	}

	// 5. Retrieve RAG context if available
	ragContext := ""
	if a.ragClient != nil {
		ragContext, _ = a.ragClient.RetrieveContext(ctx, msg.Text, 0, 3)
	}

	// 6. Build system prompt
	systemPrompt := buildWASystemPrompt(ragContext)

	// 7. Generate AI response
	reply, err := a.llm.GenerateResponse(ctx, systemPrompt, chatHistory, 200)
	if err != nil {
		a.log.Warn("wa agent: LLM failed", zap.Error(err))
		return "", nil
	}

	// 8. Check for human takeover signal
	if strings.Contains(reply, humanTakeover) {
		reply = strings.ReplaceAll(reply, humanTakeover, "")
		reply = strings.TrimSpace(reply)
		a.log.Info("wa agent: human takeover triggered", zap.Int64("conv_id", convID))
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", nil
	}

	// 9. Save outbound message
	if _, err := a.db.SaveWAMessage(convID, "outbound", reply, "text", ""); err != nil {
		a.log.Warn("wa agent: SaveWAMessage outbound", zap.Error(err))
	}

	return reply, nil
}

func buildWASystemPrompt(ragContext string) string {
	base := `You are a helpful WhatsApp sales assistant. Be concise, friendly, and professional.
Keep responses under 3 sentences unless the user asks a detailed question.
If you cannot help, say so briefly and offer to connect them with a human agent by saying ` + humanTakeover + `.`

	if ragContext != "" {
		base += "\n\nRelevant context from knowledge base:\n" + ragContext
	}
	return base
}
