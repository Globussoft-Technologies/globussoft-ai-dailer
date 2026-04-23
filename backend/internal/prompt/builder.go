// Package prompt builds system prompts for voice calls and WhatsApp agents.
package prompt

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// CallContext holds all variables needed to build a call system prompt.
type CallContext struct {
	SystemPrompt string
	GreetingText string
	// Voice config — populated from campaign/org voice settings
	TTSProvider string
	TTSVoiceID  string
	TTSLanguage string
	AgentName   string // org name, used for WA/email confirmations
}

// Builder constructs voice call prompts from DB state.
type Builder struct {
	db *db.DB
}

// NewBuilder creates a Builder.
func NewBuilder(database *db.DB) *Builder {
	return &Builder{db: database}
}

// BuildCallContext assembles the full system prompt for a voice call.
// This replaces the gRPC InitializeCall Python call.
func (b *Builder) BuildCallContext(_ context.Context, orgID, campaignID, leadID int64, language string) (*CallContext, error) {
	// Fetch organization
	var orgName string
	if org, err := b.db.GetOrganizationByID(orgID); err == nil && org != nil {
		orgName = org.Name
	}

	// Fetch custom system prompt (org-level override)
	customPrompt, _ := b.db.GetOrgSystemPrompt(orgID)

	// Fetch campaign name
	var campaignName string
	if campaignID > 0 {
		if campaign, err := b.db.GetCampaignByID(campaignID); err == nil && campaign != nil {
			campaignName = campaign.Name
		}
	}

	// Fetch lead details
	var leadName, leadInterest string
	if leadID > 0 {
		if lead, err := b.db.GetLeadByID(leadID); err == nil && lead != nil {
			leadName = strings.TrimSpace(lead.FirstName + " " + lead.LastName)
			leadInterest = lead.Interest
		}
	}

	// Fetch first product for context
	var productName, productContext string
	if products, err := b.db.GetProductsByOrg(orgID); err == nil && len(products) > 0 {
		p := products[0]
		productName = p.Name
		productContext = strings.TrimSpace(p.AgentPersona + "\n" + p.CallFlowInstructions + "\n" + p.ManualNotes)
	}

	// Build system prompt
	var systemPrompt string
	if customPrompt != "" {
		systemPrompt = customPrompt
	} else {
		systemPrompt = buildDefaultPrompt(orgName, productName, productContext, campaignName, language)
	}

	// Inject lead context
	systemPrompt = injectLeadContext(systemPrompt, leadName, leadInterest)

	greeting := buildGreeting(leadName, productName, language)

	// Fetch TTS voice config (campaign → org fallback)
	var vs db.VoiceSettings
	if campaignID > 0 {
		vs, _ = b.db.GetCampaignVoiceSettings(campaignID)
	} else {
		vs, _ = b.db.GetOrganizationVoiceSettings(orgID)
	}

	return &CallContext{
		SystemPrompt: systemPrompt,
		GreetingText: greeting,
		TTSProvider:  vs.TTSProvider,
		TTSVoiceID:   vs.TTSVoiceID,
		TTSLanguage:  vs.TTSLanguage,
		AgentName:    coalesce(orgName, "Callified AI"),
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func buildDefaultPrompt(orgName, productName, productContext, campaignName, language string) string {
	lang := languageLabel(language)
	prompt := fmt.Sprintf(`You are a professional sales agent for %s calling on behalf of their %s campaign.
Product/Service: %s

%s

Instructions:
- Speak in %s. Be concise, warm, and professional.
- Your goal is to understand the customer's needs and book an appointment or close the sale.
- Keep responses to 1–2 sentences. Never lecture.
- If the customer wants to reschedule, suggest a time. Say [HANGUP] when the call should end.
- Never reveal you are an AI. If asked directly, deflect naturally.
- Do NOT make up facts. Only use the information provided above.`,
		coalesce(orgName, "our company"),
		coalesce(campaignName, "sales"),
		coalesce(productName, "our product/service"),
		productContext,
		lang,
	)
	return prompt
}

func injectLeadContext(prompt, leadName, interest string) string {
	if leadName != "" {
		if !strings.Contains(prompt, leadName) {
			prompt += fmt.Sprintf("\n\nYou are speaking with %s.", leadName)
		}
	}
	if interest != "" {
		prompt += fmt.Sprintf("\nLead interest: %s", interest)
	}
	return prompt
}

func buildGreeting(leadName, productName, language string) string {
	firstName := firstWord(leadName)
	product := coalesce(productName, "our service")
	switch language {
	case "hi":
		if firstName != "" {
			return fmt.Sprintf("नमस्ते %s जी! मैं आपसे %s के बारे में बात करना चाहता था।", firstName, product)
		}
		return fmt.Sprintf("नमस्ते! मैं आपसे %s के बारे में बात करना चाहता था।", product)
	case "mr":
		if firstName != "" {
			return fmt.Sprintf("नमस्कार %s! मी %s बद्दल बोलण्यासाठी फोन केला आहे.", firstName, product)
		}
		return fmt.Sprintf("नमस्कार! मी %s बद्दल बोलण्यासाठी फोन केला आहे.", product)
	default:
		if firstName != "" {
			return fmt.Sprintf("Hello %s! I'm calling to tell you about %s.", firstName, product)
		}
		return fmt.Sprintf("Hello! I'm calling to tell you about %s.", product)
	}
}

func languageLabel(code string) string {
	m := map[string]string{
		"hi": "Hindi", "mr": "Marathi", "en": "English",
		"ta": "Tamil", "te": "Telugu", "kn": "Kannada",
		"bn": "Bengali", "gu": "Gujarati", "pa": "Punjabi",
	}
	if l, ok := m[code]; ok {
		return l
	}
	return "English"
}

var spaceRE = regexp.MustCompile(`\s+`)

func firstWord(s string) string {
	parts := spaceRE.Split(strings.TrimSpace(s), 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
