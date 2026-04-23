package prompt

import (
	"context"
	"fmt"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// WABuilder builds WhatsApp agent system prompts.
type WABuilder struct {
	db *db.DB
}

// NewWABuilder creates a WABuilder.
func NewWABuilder(database *db.DB) *WABuilder {
	return &WABuilder{db: database}
}

// BuildWAContext builds the system prompt for the WhatsApp AI agent.
func (b *WABuilder) BuildWAContext(_ context.Context, orgID, productID int64, chatHistory []db.WAMessage) string {
	var orgName, productName, productInfo string

	if org, err := b.db.GetOrganizationByID(orgID); err == nil && org != nil {
		orgName = org.Name
	}

	if productID > 0 {
		if p, err := b.db.GetProductByID(productID); err == nil && p != nil {
			productName = p.Name
			productInfo = strings.TrimSpace(p.AgentPersona + "\n" + p.CallFlowInstructions + "\n" + p.ManualNotes)
		}
	} else {
		// Use first product for org
		if products, err := b.db.GetProductsByOrg(orgID); err == nil && len(products) > 0 {
			productName = products[0].Name
			productInfo = strings.TrimSpace(products[0].AgentPersona + "\n" + products[0].ManualNotes)
		}
	}

	// Summarize recent chat history for context
	var recentMessages strings.Builder
	limit := len(chatHistory)
	if limit > 5 {
		limit = 5
	}
	start := len(chatHistory) - limit
	if start < 0 {
		start = 0
	}
	for _, msg := range chatHistory[start:] {
		dir := "Customer"
		if msg.Direction == "outbound" {
			dir = "Agent"
		}
		recentMessages.WriteString(fmt.Sprintf("%s: %s\n", dir, msg.MessageText))
	}

	prompt := fmt.Sprintf(`You are a WhatsApp sales assistant for %s.
Product/Service: %s
%s

Guidelines:
- Be friendly and concise (1–3 sentences per reply).
- Answer product questions accurately. Do NOT make up information.
- Help the customer book an appointment or make a purchase.
- If you cannot assist, offer to connect them with a human agent.`,
		coalesce(orgName, "our company"),
		coalesce(productName, "our product/service"),
		productInfo,
	)

	if rc := strings.TrimSpace(recentMessages.String()); rc != "" {
		prompt += "\n\nRecent conversation:\n" + rc
	}
	return prompt
}
