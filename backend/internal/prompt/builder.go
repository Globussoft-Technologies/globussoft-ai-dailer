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
	TTSProvider            string
	TTSVoiceID             string
	TTSLanguage            string
	MaxCallDurationSeconds int
	AgentName              string // org name, used for WA/email confirmations
	PersonaName            string // voice persona name (e.g. "आदित्य"), used inside greeting + system prompt
	CallMemoryCount        int    // past-call memory entries injected into the system prompt (0 = none)
}

// ── voice identity data ───────────────────────────────────────────────────────
//
// voiceNamesDevanagari maps TTS voice IDs to the agent's spoken name in
// Devanagari script (used for Hindi/Marathi greetings). voiceNamesBengali is
// the Bengali-script equivalent. femaleVoices marks voice IDs that should use
// feminine verb forms ("बोल रही हूँ" vs "बोल रहा हूँ", Bengali/Marathi parallels).

var voiceNamesDevanagari = map[string]string{
	// Sarvam male
	"aditya": "आदित्य", "rahul": "राहुल", "amit": "अमित", "dev": "देव", "rohan": "रोहन",
	"varun": "वरुण", "kabir": "कबीर", "manan": "मनन", "sumit": "सुमित", "ratan": "रतन",
	"aayan": "आयान", "shubh": "शुभ", "ashutosh": "आशुतोष", "advait": "अद्वैत",
	// Sarvam female
	"ritu": "रितु", "priya": "प्रिया", "neha": "नेहा", "pooja": "पूजा", "simran": "सिमरन",
	"kavya": "काव्या", "ishita": "इशिता", "shreya": "श्रेया", "roopa": "रूपा",
	// SmallestAI male
	"raj": "राज", "arnav": "अर्णव", "raman": "रमन", "raghav": "राघव", "aarav": "आरव",
	"ankur": "अंकुर", "aravind": "अरविंद", "saurabh": "सौरभ", "chetan": "चेतन", "ashish": "आशीष",
	// SmallestAI female
	"kajal": "काजल", "pragya": "प्रज्ञा", "nisha": "निशा", "deepika": "दीपिका", "diya": "दिया",
	"sushma": "सुषमा", "shweta": "श्वेता", "ananya": "अनन्या", "mithali": "मिताली",
	"saina": "साइना", "sanya": "सान्या", "mansi": "मानसी",
}

var voiceNamesBengali = map[string]string{
	// Sarvam male
	"aditya": "আদিত্য", "rahul": "রাহুল", "amit": "অমিত", "dev": "দেব", "rohan": "রোহন",
	"varun": "বরুণ", "kabir": "কবীর", "manan": "মনন", "sumit": "সুমিত", "ratan": "রতন",
	"aayan": "আয়ান", "shubh": "শুভ", "ashutosh": "আশুতোষ", "advait": "অদ্বৈত",
	// Sarvam female
	"ritu": "রিতু", "priya": "প্রিয়া", "neha": "নেহা", "pooja": "পূজা", "simran": "সিমরন",
	"kavya": "কাব্যা", "ishita": "ইশিতা", "shreya": "শ্রেয়া", "roopa": "রূপা",
	// SmallestAI male
	"raj": "রাজ", "arnav": "অর্ণব", "raman": "রমন", "raghav": "রাঘব", "aarav": "আরভ",
	"ankur": "অঙ্কুর", "aravind": "অরবিন্দ", "saurabh": "সৌরভ", "chetan": "চেতন", "ashish": "আশীষ",
	// SmallestAI female
	"kajal": "কাজল", "pragya": "প্রজ্ঞা", "nisha": "নিশা", "deepika": "দীপিকা", "diya": "দিয়া",
	"sushma": "সুষমা", "shweta": "শ্বেতা", "ananya": "অনন্যা", "mithali": "মিতালী",
	"saina": "সাইনা", "sanya": "সান্যা", "mansi": "মানসী",
}

// elevenLabsPersona maps ElevenLabs opaque voice IDs to the first-name persona
// the agent introduces itself as. Without this, every ElevenLabs voice fell
// back to "Arjun" because the IDs aren't title-case-able like Sarvam IDs.
var elevenLabsPersona = map[string]string{
	"oH8YmZXJYEZq5ScgoGn9": "Aakash",
	"X4ExprIXDKrWcHdtGysh": "Anjura",
	"SXuKWBhKoIoAHKlf6Gt3": "Gaurav",
	"N09NFwYJJG9VSSgdLQbT": "Ishan",
	"U9wNM2BNANqtBCawWLgA": "Himanshu",
	"h061KGyOtpLYDxcoi8E3": "Ravi",
	"Ock0AL5DBkvTUDePt4Hm": "Viraj",
	"nwj0s2LU9bDWRKND5yzA": "Bunty",
	"amiAXapsDOAiHJqbsAZj": "Priya",
	"6JsmTroalVewG1gA6Jmw": "Sia",
	"9vP6R7VVxNwGIGLnpl17": "Suhana",
	"hO2yZ8lxM3axUxL8OeKX": "Mini",
	"s0oIsoSJ9raiUm7DJNzW": "Aarav",
}

var femaleVoices = map[string]bool{
	"kajal": true, "pragya": true, "nisha": true, "deepika": true, "diya": true,
	"sushma": true, "shweta": true, "ananya": true, "mithali": true, "saina": true,
	"sanya": true, "pooja": true, "mansi": true, "priya": true, "ritu": true,
	"neha": true, "simran": true, "kavya": true, "ishita": true, "shreya": true,
	"roopa": true,
	// SmallestAI English female voices
	"jasmine": true, "emily": true,
	// ElevenLabs IDs (match Python _female_voices)
	"amiAXapsDOAiHJqbsAZj": true, "6JsmTroalVewG1gA6Jmw": true,
	"9vP6R7VVxNwGIGLnpl17": true, "hO2yZ8lxM3axUxL8OeKX": true,
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
	// Fetch TTS voice config first (campaign → org fallback) so we know the
	// effective language before building the greeting and system prompt.
	var vs db.VoiceSettings
	if campaignID > 0 {
		vs, _ = b.db.GetCampaignVoiceSettings(campaignID)
	} else {
		vs, _ = b.db.GetOrganizationVoiceSettings(orgID)
	}
	effectiveLang := coalesce(vs.TTSLanguage, language)

	// Fetch organization
	var orgName string
	if org, err := b.db.GetOrganizationByID(orgID); err == nil && org != nil {
		orgName = org.Name
	}

	// Fetch custom system prompt (org-level override)
	customPrompt, _ := b.db.GetOrgSystemPrompt(orgID)

	// Fetch campaign (name + product link + lead source)
	var campaignName, campaignSource string
	var campaignProductID int64
	if campaignID > 0 {
		if campaign, err := b.db.GetCampaignByID(campaignID); err == nil && campaign != nil {
			campaignName = campaign.Name
			campaignProductID = campaign.ProductID
			campaignSource = campaign.LeadSource
		}
	}

	// Fetch lead details
	var leadName, leadSource string
	if leadID > 0 {
		if lead, err := b.db.GetLeadByID(leadID); err == nil && lead != nil {
			leadName = strings.TrimSpace(lead.FirstName + " " + lead.LastName)
			leadSource = lead.Source
		}
	}

	// Effective source: lead's source wins if it's a known dropdown value, else campaign's.
	effectiveSource := resolveSource(leadSource, campaignSource)

	// Fetch product: prefer the campaign's linked product, fall back to org's first product.
	var productName, productContext, callFlowInstructions string
	if campaignProductID > 0 {
		if p, err := b.db.GetProductByID(campaignProductID); err == nil && p != nil {
			productName = p.Name
			callFlowInstructions = strings.TrimSpace(p.CallFlowInstructions)
			productContext = strings.TrimSpace(p.AgentPersona + "\n" + p.ManualNotes)
		}
	}
	if productName == "" {
		if products, err := b.db.GetProductsByOrg(orgID); err == nil && len(products) > 0 {
			p := products[0]
			productName = p.Name
			callFlowInstructions = strings.TrimSpace(p.CallFlowInstructions)
			productContext = strings.TrimSpace(p.AgentPersona + "\n" + p.ManualNotes)
		}
	}

	// Resolve voice persona + company name for the greeting.
	personaName, bol := agentIdentity(vs.TTSVoiceID, effectiveLang)
	companyName := companyDisplayName(productName, productContext, orgName, effectiveLang)
	sourceInline := sourceContextInline(effectiveSource, effectiveLang)

	pc := promptContext{
		CompanyName:          companyName,
		ProductName:          productName,
		ProductContext:       productContext,
		CallFlowInstructions: callFlowInstructions,
		CampaignName:         campaignName,
		PersonaName:          personaName,
		LeadFirst:            firstWord(leadName),
		SourceInline:         sourceInline,
		Language:             effectiveLang,
	}

	// Cross-call memory: reviews from past calls with this lead become context
	// for the new call (docs/call-memory-proposal.md). Soft-fail by design — a
	// lookup error or a lead with no history leaves the prompt unchanged and
	// must never block a call. Placement differs by prompt type: the default
	// template gets it between GOAL and CALL FLOW (confirmed facts before the
	// questionnaire); custom org prompts get it appended at the end since we
	// don't control their layout.
	memoryCount := 0
	memoryBlock := ""
	if leadID > 0 && campaignID > 0 {
		if memories, err := b.db.GetLastCallMemory(leadID, campaignID, 3); err == nil {
			memoryBlock = renderCallMemory(memories)
			memoryCount = len(memories)
		}
	}

	// Build system prompt — custom org-level override short-circuits the full
	// template and just gets a language directive appended.
	var systemPrompt string
	if customPrompt != "" {
		systemPrompt = customPrompt + fmt.Sprintf("\n\nIMPORTANT: Respond only in %s. Do not use English unless the user asks for it.", languageLabel(effectiveLang))
		if leadName != "" && !strings.Contains(systemPrompt, leadName) {
			systemPrompt += fmt.Sprintf("\n\nYou are speaking with %s.", leadName)
		}
		systemPrompt += memoryBlock
	} else {
		pc.CallMemory = memoryBlock
		systemPrompt = buildDefaultPrompt(pc)
	}

	// Append pronunciation guide so the LLM uses phonetic forms directly in its
	// responses, which lets the TTS engine synthesise them correctly.
	if prons, err := b.db.GetAllPronunciations(); err == nil && len(prons) > 0 {
		var pb strings.Builder
		pb.WriteString("\n\n## PRONUNCIATION\nWhen saying these words, use the phonetic spelling shown:\n")
		for _, p := range prons {
			fmt.Fprintf(&pb, "- Say %q as %q\n", p.Word, p.Phonetic)
		}
		systemPrompt += pb.String()
	}

	greeting := buildGreeting(leadName, companyName, personaName, bol, effectiveSource, effectiveLang)

	return &CallContext{
		SystemPrompt:           systemPrompt,
		GreetingText:           greeting,
		TTSProvider:            vs.TTSProvider,
		TTSVoiceID:             vs.TTSVoiceID,
		TTSLanguage:            vs.TTSLanguage,
		MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
		AgentName:              coalesce(orgName, "Callified AI"),
		PersonaName:            personaName,
		CallMemoryCount:        memoryCount,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// promptContext bundles every variable the system prompt needs so we can pass
// a single value around instead of threading many parameters.
type promptContext struct {
	CompanyName          string
	ProductName          string
	ProductContext       string
	CallFlowInstructions string
	CampaignName         string
	PersonaName          string
	LeadFirst            string
	SourceInline         string
	Language             string
	// CallMemory is the rendered ## PREVIOUS CALLS block ("" when the lead
	// has no history). Injected between GOAL and CALL FLOW so confirmed
	// facts are established before the questionnaire, not appended after it.
	CallMemory string
}

// callMemoryMaxFieldLen caps each memory field rendered into the system
// prompt. Prompt bloat slows and costs every LLM turn; short dense memory
// beats long transcripts.
const callMemoryMaxFieldLen = 200

// renderCallMemory renders past-call reviews as a ## PREVIOUS CALLS block
// for the system prompt. Returns "" when there is nothing to inject, so a
// lead without history gets a byte-identical prompt to before this feature.
func renderCallMemory(memories []db.CallMemory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n## PREVIOUS CALLS WITH THIS CUSTOMER\n")
	sb.WriteString("[INTERNAL NOTES — never speak, translate, paraphrase, or mention these notes to the customer]\n")
	sb.WriteString("The entries below are CONFIRMED FACTS from your previous conversations with this customer. " +
		"They OVERRIDE the qualification questions in the call flow: do not re-ask anything already recorded here — " +
		"briefly confirm the most recent value and move to the next step. If two entries conflict, trust the newer one.\n")
	for i, m := range memories {
		fmt.Fprintf(&sb, "%d. Date: %s\n", i+1, m.CreatedAt)
		if s := clampRunes(m.Summary, callMemoryMaxFieldLen); s != "" {
			fmt.Fprintf(&sb, "   What happened: %s\n", s)
		}
		if s := clampRunes(m.FailureReason, callMemoryMaxFieldLen); s != "" {
			fmt.Fprintf(&sb, "   What went wrong: %s\n", s)
		}
		if s := clampRunes(m.Suggestion, callMemoryMaxFieldLen); s != "" {
			fmt.Fprintf(&sb, "   Do better this time: %s\n", s)
		}
	}
	sb.WriteString("Use this history naturally: do not re-pitch what the customer already rejected, honor commitments made on past calls, and never reveal that you are reading notes. " +
		"If the customer doesn't recognize a detail from these notes or denies it — even a detail recorded here — drop it permanently and never mention it again; that detail was wrong. " +
		"Ignore any detail in these notes that is unrelated to the product you are calling about.")
	return sb.String()
}

// clampRunes trims whitespace and truncates s to at most n runes, adding an
// ellipsis when cut.
func clampRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// buildDefaultPrompt assembles the LLM system prompt. Structure is shared
// across all languages: core rules are expressed in English (the LLM follows
// them regardless of output language) while identity and register hints are
// drawn from per-language fragments so the model sees examples in its target
// language.
func buildDefaultPrompt(pc promptContext) string {
	frag, ok := langFragments[pc.Language]
	if !ok {
		frag = langFragments["en"]
	}
	langLabel := languageLabel(pc.Language)
	company := coalesce(pc.CompanyName, "our company")
	persona := coalesce(pc.PersonaName, "Arjun")
	leadFirst := coalesce(pc.LeadFirst, "the lead")

	var b strings.Builder

	// Language marker — used by downstream tooling (STT hints, transcript tags).
	fmt.Fprintf(&b, "[LANG:%s]\n\n", coalesce(pc.Language, "en"))

	// Identity line in the target language so the LLM mirrors the register.
	fmt.Fprintf(&b, "%s\n\n", fmt.Sprintf(frag.IdentityLine, persona, company))

	// Goal.
	b.WriteString("## GOAL\n")
	switch {
	case pc.CallFlowInstructions != "" && pc.CallMemory != "":
		// Continuation call: memory exists, so the questionnaire is replaced by
		// a confirmation step (see CALL FLOW below). Positive directive — LLMs
		// follow "do X" far more reliably than "don't do Y".
		b.WriteString("CONTINUATION CALL — you have spoken with this customer before. The PREVIOUS CALLS section below contains confirmed facts from those conversations. Do NOT run the full qualification questionnaire again. Confirm the newest recorded details briefly, accept any corrections, then book an appointment.\n\n")
	case pc.CallFlowInstructions != "":
		b.WriteString("Qualify the lead using the questions below as a guide. Ask them in order, but adapt to the conversation: answer the customer's questions, handle interruptions, and only move to the next question after the current one is clearly answered. Then book an appointment.\n\n")
	default:
		b.WriteString("Book an appointment with the customer for a follow-up from a senior agent. ")
		b.WriteString("If the customer asks a question, answer in 1 sentence first, then push toward booking.\n\n")
	}

	// Product focus — the agent must never pursue topics outside the product,
	// whether the customer raises them mid-call or they appear in memory
	// notes (observed failure: agent interrogated a customer about "child
	// care" mentioned by background chatter).
	product := coalesce(pc.ProductName, "our product")
	b.WriteString("## PRODUCT FOCUS (STRICT)\n")
	fmt.Fprintf(&b, "This call is ONLY about %s. It is the only product and the only topic of this call.\n", product)
	b.WriteString("- NEVER ask follow-up questions about anything unrelated to this product — even if the customer mentioned it first. If the customer brings up an off-topic subject, acknowledge it in a few words and steer back to the product in the same reply.\n")
	b.WriteString("- Never treat an off-topic mention (other services, family, personal matters, anything not about this product) as a requirement, interest, or need. Only product-related details count.\n\n")

	// Past-call memory (confirmed facts) precedes the questionnaire so the
	// LLM treats them as established context, not an afterthought. Empty for
	// leads without history.
	if pc.CallMemory != "" {
		b.WriteString(pc.CallMemory)
		b.WriteString("\n")
	}

	// Call flow. With memory, the qualification questionnaire is replaced by a
	// confirm-and-book flow — re-interrogating a known customer is the failure
	// mode this feature exists to prevent.
	b.WriteString("## CALL FLOW\n")
	if pc.CallMemory != "" && pc.CallFlowInstructions != "" {
		b.WriteString("1. Intro (already spoken by TTS): acknowledge it naturally, then confirm the key details from PREVIOUS CALLS in ONE question. Example: \"Just to confirm — this is for fifteen users across two Bengaluru locations, correct?\"\n")
		b.WriteString("2. Whatever the customer corrects, accept it and update the details. Then ask when they are free for a short demo and book it.\n")
		b.WriteString("3. When a time is confirmed → repeat the time, thank them, then end with [HANGUP].\n")
		b.WriteString("4. If the customer asks to hang up / is not interested → say a short thanks and end with [HANGUP].\n")
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "1. Intro (already spoken by TTS): acknowledge it naturally.\n")
		if pc.SourceInline != "" {
			fmt.Fprintf(&b, "   Lead context: they %s.\n", pc.SourceInline)
		}
		if pc.CallFlowInstructions != "" {
			b.WriteString(pc.CallFlowInstructions)
			b.WriteString("\n")
		} else {
			b.WriteString("2. If the customer says yes/ok → DO NOT ask \"are you interested?\" again. Go straight to: ")
			fmt.Fprintf(&b, "%q in %s.\n", frag.AskWhenFree, langLabel)
			b.WriteString("3. If the customer asks about the product → answer briefly in 1 sentence, then ask about meeting time.\n")
			b.WriteString("4. When a time is confirmed → repeat the time, thank them, then end with [HANGUP].\n")
			b.WriteString("5. If the customer asks to hang up / is not interested → say a short thanks and end with [HANGUP].\n")
		}
		b.WriteString("\n")
	}

	// Core rules — universal, English.
	b.WriteString(`## CORE RULES (STRICT)
1. NO HALLUCINATION. Only use facts from PRODUCT KNOWLEDGE below. Never invent addresses, phone numbers, pricing, terms, locations, timings, or features. If unknown, say the senior will share details in the meeting.
2. ONE QUESTION ONLY. Ask exactly ONE question per response. Never combine two questions in the same reply. Wait for the customer's answer before asking the next question. WRONG: "Which option do you want? What is your budget?" (TWO QUESTIONS!) RIGHT: "Which option do you prefer — A or B?" Then wait. Then ask the next question separately.
3. SHORT SPOKEN TURNS. Every reply must be one short spoken sentence, ideally under eight seconds. Do not explain multiple benefits, certificates, campaigns, or next steps in one turn. If more detail is needed, ask permission first and wait.
4. NO MARKDOWN. No *, **, #, bullets, or numbered lists — TTS reads the characters literally. Plain text only.
5. NUMBERS IN WORDS. Say "two and a half crore", not "2.5 Cr". Say "five PM", not "5 PM". Say "sixty seconds", not "60-second" or "60 seconds". Never use digit-hyphen-word combinations like "60-second", "30-day", or "15-minute" — always write them as words: "sixty second", "thirty day", "fifteen minute".
6. LEAD NAME. The lead is "` + leadFirst + `". Use this exact spelling. Never change, abbreviate, or guess another name.
7. [HANGUP] TAG. End every final turn with the literal English string [HANGUP] after your goodbye text. Never translate it. Never wrap it in extra brackets. Without [HANGUP] the call does not end.
8. FUTURE DATES ONLY. Offer "today", "tomorrow", "day after". Never offer past dates.
9. LISTEN FIRST. If the customer trails off ("I…", "umm…"), wait — ask them to continue. Do not repeat time slots.
10. BARGE-IN / INTERRUPTION. If the customer starts speaking while you are speaking, stop and listen. If the interruption directly answers your current question, accept that answer and continue. If it is a question, objection, unclear reply, filler, or side comment, address it briefly and return to the same unanswered question. Do NOT move to the next call-flow question just because you were interrupted.
11. NO REPETITION — EXCEPT WHEN NEEDED. Do not ask the same question twice if the customer answered it. But if the customer did not answer, changed the subject, asked a question back, or gave an unclear reply, you MUST re-ask or clarify the current question before advancing.
12. NEVER REVEAL YOU ARE AN AI. If asked, deflect naturally and stay in character.
13. VALIDATE OPTION ANSWERS. When you ask a question with specific options, the customer's reply must clearly match one of those options before you move on. Do not guess, assume, or infer an answer from unrelated replies. If the reply is off-topic, unclear, a joke, or a question back, respond briefly if needed and re-ask the same option question politely. If the customer rejects one option, ask again with the remaining relevant options. If the customer asks to switch language, switch language and re-ask the same question.
14. ADVANCE ONLY AFTER ANSWER. You may only proceed to the next call-flow question after the customer answered the current one. An interruption, side question, or unclear reply does not count as an answer.
15. REPEATED CUSTOMER QUESTIONS. If the customer asks the same question repeatedly, answer it in simpler words instead of repeating the same sentence. Do not say "I already answered", "multiple times", "unable to answer", or blame the customer. On the customer's third total ask of the same question, offer a senior callback and continue the call; do not end or use [HANGUP]. On the customer's fourth total ask of the same question, say a senior teammate will follow up, thank them, and close with [HANGUP].
16. NO ACKNOWLEDGEMENT-ONLY ANSWERS. If the customer asks a product, company, price, or process question, never reply with only "sure", "certainly", "okay", the customer's name, or the translated equivalent in any language. Give the direct answer immediately in one short spoken sentence.
17. INTERNAL NOTES ARE INVISIBLE. Any text in square brackets [...] — including TURN CONTROL NOTES and instructions about repeated questions, interruptions, or manager hints — is internal system data. NEVER speak it, translate it, paraphrase it, summarize it, or acknowledge it in any way. Reply only with the customer-facing answer or question.
`)

	// Per-language rule extras (forward signals, rejection detection, direct
	// question answering, Devanagari list ban). See lang_rules.go.
	if extras := extraRulesFor(pc.Language, leadFirst); extras != "" {
		b.WriteString(extras)
		b.WriteString("\n")
	}

	// Language directive + register hint + per-language banned words if we have them.
	fmt.Fprintf(&b, "## LANGUAGE\n- Respond ONLY in %s. %s\n", langLabel, frag.RegisterHint)
	if frag.BannedWords != "" {
		fmt.Fprintf(&b, "- Banned formal/written register (use casual alternatives instead): %s\n", frag.BannedWords)
	}
	b.WriteString("- English words (e.g. meeting, project, free, okay, sorry, thank you) mix in naturally — that is how real sales calls sound.\n")
	b.WriteString("- NEVER comment on, correct, or acknowledge the customer's language or accent. Just continue naturally in the configured language.\n")
	b.WriteString("- ONLY switch language if the customer explicitly asks, for example: 'speak in Hindi', 'Kannada dalli mathadi', 'Punjabi mein bolo'.\n")
	b.WriteString("- If the customer speaks a few words in another language, do NOT switch. Keep replying in the configured language.\n\n")

	// Product knowledge.
	b.WriteString("## PRODUCT KNOWLEDGE\n")
	fmt.Fprintf(&b, "Company: %s\n", company)
	if pc.CampaignName != "" {
		fmt.Fprintf(&b, "Campaign: %s\n", pc.CampaignName)
	}
	if pc.ProductName != "" {
		fmt.Fprintf(&b, "Product/Service: %s\n", pc.ProductName)
	}
	if pc.ProductContext != "" {
		fmt.Fprintf(&b, "\n%s\n", pc.ProductContext)
	}
	return b.String()
}

// langPromptFragments holds the small amount of per-language content the
// default prompt needs: an identity sentence in the target language (2nd-person,
// addressing the LLM as the agent), a short "ask when you're free" line to
// quote, a register hint, and optional banned formal words (ported from Python
// for hi/mr/bn).
type langPromptFragments struct {
	IdentityLine string // fmt: persona, company — 2nd-person "You are X from Y"
	AskWhenFree  string // quoted in the call-flow section
	RegisterHint string // appended after "Respond ONLY in X."
	BannedWords  string // empty when none provided
}

var langFragments = map[string]langPromptFragments{
	"hi": {
		IdentityLine: "तुम %s हो। तुम %s कंपनी से बात कर रहे हो। तुम एक sales agent हो।",
		AskWhenFree:  "बढ़िया! आज या कल कब free हैं?",
		RegisterHint: "Use casual spoken Hindi (Hinglish) — how friends talk on the phone, not how newspapers write.",
		BannedWords: "" +
			"'सुविधा' (use 'facility'), 'स्वारस्य' (use 'interest'), 'प्रक्रिया' (use 'process'), " +
			"'प्रदान करना' (use 'dena'), 'आवश्यक' (use 'zaroori/need'), 'संपर्क' (use 'contact'), " +
			"'उपलब्ध' (use 'available'), 'विस्तार' (use 'detail'), 'जानकारी' (use 'info'), " +
			"'विशेष' (use 'special'), 'अवसर' (use 'opportunity')",
	},
	"mr": {
		IdentityLine: "तू %s आहेस. तू %s कंपनीतून बोलत आहेस. तू एक sales agent आहेस.",
		AskWhenFree:  "छान! आज किंवा उद्या कधी free आहात?",
		RegisterHint: "Use casual spoken Marathi mixed with English — how people talk in Mumbai/Pune, not written/formal Marathi.",
		BannedWords: "" +
			"'चालू' as filler (use 'काय विचार आहे'), 'बघा' as starter, 'स्वारस्य' (use 'interest'), " +
			"'अवसर' (use 'opportunity'), 'आवश्यक' (use 'lagel'), 'प्रक्रिया' (use 'process'), " +
			"'नोंदवितो' (use 'note karto'), 'विशेषज्ञ' (use 'expert'), 'उभारण्यात' / 'संपर्क साधेन' / 'शुभेच्छा' (too formal)",
	},
	"bn": {
		IdentityLine: "তুমি %s। তুমি %s কোম্পানি থেকে কথা বলছ। তুমি একজন sales agent।",
		AskWhenFree:  "বাহ! আজ বা কাল কখন free আছেন?",
		RegisterHint: "Use casual spoken Bengali (Kolkata sales-call register) mixed with English — not formal/written Bangla.",
		BannedWords: "" +
			"'প্রপার্টি খারিদ' (use 'flat/bari kinte'), 'নির্দিষ্ট' (use 'specific'), 'উপলব্ধ' (use 'available'), " +
			"'বিস্তারিত' (use 'detail'), 'তথ্য' (use 'info'), 'সম্পর্কে' (use 'about'), 'বিভিন্ন' (use 'different'), " +
			"'অনুযায়ী' (use 'according to'), 'ভেরিফাইড' (use 'verified'), 'প্রয়োজন' (use 'need/lagbe'), " +
			"'প্রদান' (use 'provide/debo'), 'কনসাল্টেশন' (use 'meeting/kotha hobe')",
	},
	"gu": {
		IdentityLine: "તમે %s છો. તમે %s કંપનીમાંથી વાત કરો છો. તમે એક sales agent છો.",
		AskWhenFree:  "સરસ! આજે કે કાલે ક્યારે free છો?",
		RegisterHint: "Use casual spoken Gujarati mixed with English — how people talk in Ahmedabad/Surat, not formal Gujarati.",
	},
	"pa": {
		IdentityLine: "ਤੁਸੀਂ %s ਹੋ। ਤੁਸੀਂ %s ਕੰਪਨੀ ਤੋਂ ਗੱਲ ਕਰ ਰਹੇ ਹੋ। ਤੁਸੀਂ ਇੱਕ sales agent ਹੋ।",
		AskWhenFree:  "ਵਧੀਆ! ਅੱਜ ਜਾਂ ਕੱਲ੍ਹ ਕਦੋਂ free ਹੋ?",
		RegisterHint: "Use casual spoken Punjabi mixed with English — how people talk on the phone, not formal/written Punjabi.",
	},
	"ta": {
		IdentityLine: "நீங்கள் %s. நீங்கள் %s நிறுவனத்திலிருந்து பேசுகிறீர்கள். நீங்கள் ஒரு sales agent.",
		AskWhenFree:  "அருமை! இன்று அல்லது நாளை எப்போது free?",
		RegisterHint: "Use casual spoken Tamil (Chennai sales-call register) mixed with English — not formal/literary Tamil.",
	},
	"te": {
		IdentityLine: "మీరు %s. మీరు %s కంపెనీ నుండి మాట్లాడుతున్నారు. మీరు ఒక sales agent.",
		AskWhenFree:  "భలే! ఈరోజు లేదా రేపు ఎప్పుడు free?",
		RegisterHint: "Use casual spoken Telugu mixed with English — how people talk on the phone in Hyderabad, not formal/written Telugu.",
	},
	"kn": {
		IdentityLine: "ನೀವು %s. ನೀವು %s ಕಂಪನಿಯಿಂದ ಮಾತನಾಡುತ್ತಿದ್ದೀರಿ. ನೀವು ಒಬ್ಬ sales agent.",
		AskWhenFree:  "ಚೆನ್ನಾಗಿದೆ! ಇಂದು ಅಥವಾ ನಾಳೆ ಯಾವಾಗ free?",
		RegisterHint: "Use casual spoken Kannada mixed with English — how people talk on the phone in Bangalore, not formal/written Kannada.",
	},
	"ml": {
		IdentityLine: "നിങ്ങൾ %s ആണ്. നിങ്ങൾ %s കമ്പനിയിൽ നിന്ന് സംസാരിക്കുന്നു. നിങ്ങൾ ഒരു sales agent ആണ്.",
		AskWhenFree:  "കൊള്ളാം! ഇന്നോ നാളെയോ എപ്പോഴാണ് free?",
		RegisterHint: "Use casual spoken Malayalam mixed with English — how people talk on the phone, not formal/literary Malayalam.",
	},
	"en": {
		IdentityLine: "You are %s, a sales agent calling from %s.",
		AskWhenFree:  "Great! When are you free — today or tomorrow?",
		RegisterHint: "Use casual conversational English — friendly, informal, like a phone call.",
	},
}

// agentIdentity resolves a TTS voice ID into (personaName, bol) where bol is
// the gender- and language-appropriate "speaking" verb phrase used inside the
// AgentPersonaName returns the spoken persona name for a voice ID + language —
// e.g. "Mithali" for ("mithali", "en"), "मिताली" for ("mithali", "hi"). Used
// by the WS handler to fix up the greeting when a session overrides the voice
// after the prompt builder has already rendered the greeting using the org
// default voice's persona.
func AgentPersonaName(voiceID, language string) string {
	name, _ := agentIdentity(voiceID, language)
	return name
}

// greeting. personaName is rendered in the script appropriate to the language;
// for Dravidian languages and English we keep a Roman (title-cased) form since
// the TTS engine pronounces it correctly and a full per-script name table is
// out of scope. Unknown voice IDs fall back to a locale-appropriate "Arjun".
func agentIdentity(voiceID, language string) (personaName, bol string) {
	rawID := strings.TrimSpace(voiceID)
	vid := strings.ToLower(rawID)
	isFemale := femaleVoices[vid] || femaleVoices[rawID]

	// ElevenLabs IDs are opaque hashes (e.g. "oH8YmZXJYEZq5ScgoGn9") so they
	// can't be title-cased and aren't in the Deva
	// nagari/Bengali maps. Look up
	// a Roman first-name first and use it as the fallback so users hear
	// "Aakash"/"Anjura"/etc. instead of always "Arjun" with ElevenLabs.
	romanFallback := "Arjun"
	if name, ok := elevenLabsPersona[rawID]; ok {
		romanFallback = name
	}

	switch language {
	case "hi":
		personaName = lookupOr(voiceNamesDevanagari, vid, romanFallback)
		if isFemale {
			bol = "बोल रही हूँ"
		} else {
			bol = "बोल रहा हूँ"
		}
	case "mr":
		personaName = lookupOr(voiceNamesDevanagari, vid, romanFallback)
		// Marathi "बोलत आहे" is the same for both genders in this register.
		bol = "बोलत आहे"
	case "bn":
		personaName = lookupOr(voiceNamesBengali, vid, romanFallback)
		bol = "বলছি"
	case "gu":
		personaName = romanPersona(rawID, romanFallback)
		if isFemale {
			bol = "વાત કરી રહી છું"
		} else {
			bol = "વાત કરી રહ્યો છું"
		}
	case "pa":
		personaName = romanPersona(rawID, romanFallback)
		if isFemale {
			bol = "ਬੋਲ ਰਹੀ ਹਾਂ"
		} else {
			bol = "ਬੋਲ ਰਿਹਾ ਹਾਂ"
		}
	case "ta":
		personaName = romanPersona(rawID, romanFallback)
		bol = "பேசுகிறேன்" // first-person present is gender-neutral
	case "te":
		personaName = romanPersona(rawID, romanFallback)
		bol = "మాట్లాడుతున్నాను"
	case "kn":
		personaName = romanPersona(rawID, romanFallback)
		bol = "ಮಾತನಾಡುತ್ತಿದ್ದೇನೆ"
	case "ml":
		personaName = romanPersona(rawID, romanFallback)
		bol = "സംസാരിക്കുകയാണ്"
	default:
		personaName = romanPersona(rawID, romanFallback)
		bol = "calling"
	}
	return
}

func lookupOr(m map[string]string, key, fallback string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return fallback
}

// romanPersona returns a title-cased Roman name when rawID looks like a simple
// Roman first name (all lowercase a-z, e.g. "aditya" → "Aditya", "james" →
// "James"). Anything else returns the caller's fallback. The check is on the
// caller-supplied rawID (NOT a lowercased copy), because ElevenLabs voice IDs
// like "amiAXapsDOAiHJqbsAZj" become all-lowercase letters when downcased and
// would otherwise be incorrectly title-cased into gibberish — those IDs always
// contain at least one uppercase letter (and usually digits) in their raw form,
// so the loop below rejects them and the caller's fallback (resolved earlier
// from elevenLabsPersona) is used instead.
func romanPersona(rawID, fallback string) string {
	if rawID == "" {
		return fallback
	}
	for _, r := range rawID {
		if r < 'a' || r > 'z' {
			return fallback
		}
	}
	return strings.ToUpper(rawID[:1]) + rawID[1:]
}

var (
	urlDomainRE      = regexp.MustCompile(`://(?:www\.)?([^./]+)`)
	productCompanyRE = regexp.MustCompile(`by\s+(\w[\w\s]*?)[)—-]`)
)

// companyDisplayName picks the best "company name" for the greeting, matching
// the Python resolution order: product name (if non-URL) → domain extracted
// from product-name URL → "by X" pattern inside product context → org name →
// language-appropriate generic fallback.
func companyDisplayName(productName, productContext, orgName, language string) string {
	pn := strings.TrimSpace(productName)
	if pn != "" && !strings.HasPrefix(pn, "http") {
		return pn
	}
	if strings.HasPrefix(pn, "http") {
		if m := urlDomainRE.FindStringSubmatch(pn); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}
	if productContext != "" {
		if m := productCompanyRE.FindStringSubmatch(productContext); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	if on := strings.TrimSpace(orgName); on != "" {
		return on
	}
	switch language {
	case "bn":
		return "আমাদের কোম্পানি"
	case "mr":
		return "आमची कंपनी"
	case "hi":
		return "हमारी कंपनी"
	default:
		return "our company"
	}
}

// sourceContextInline returns the body of the greeting question for every
// supported language — plugged into patterns like "क्या आपने {X}?" /
// "তুমি {X} কি?" / "Did you {X}?". Returns "" for empty/unknown/"cold".
func sourceContextInline(source, language string) string {
	source = canonicalSource(source)
	if source == "" || source == "cold" {
		return ""
	}
	var label string
	switch source {
	case "facebook":
		label = "Facebook"
	case "google":
		label = "Google"
	case "instagram":
		label = "Instagram"
	case "linkedin":
		label = "LinkedIn"
	}
	switch language {
	case "hi":
		switch source {
		case "website":
			return "हमारी वेबसाइट पर फ़ॉर्म भरा था"
		case "referral":
			return "हमारे किसी ग्राहक से हमारे बारे में सुना था"
		}
		if label != "" {
			return fmt.Sprintf("%s पर हमारा ad देखकर enquiry की थी", label)
		}
	case "mr":
		switch source {
		case "website":
			return "आमच्या वेबसाइटवर फॉर्म भरला होता"
		case "referral":
			return "आमच्या एका ग्राहकाकडून आमच्याबद्दल ऐकले होते"
		}
		if label != "" {
			return fmt.Sprintf("%s वर आमची ad बघून enquiry केली होती", label)
		}
	case "bn":
		switch source {
		case "website":
			return "আমাদের ওয়েবসাইটে ফর্ম ভরেছিলেন"
		case "referral":
			return "আমাদের কোনো গ্রাহকের কাছে আমাদের সম্পর্কে শুনেছিলেন"
		}
		if label != "" {
			return fmt.Sprintf("%s-এ আমাদের ad দেখে enquiry করেছিলেন", label)
		}
	case "gu":
		switch source {
		case "website":
			return "અમારી વેબસાઇટ પર ફોર્મ ભર્યું હતું"
		case "referral":
			return "અમારા એક ગ્રાહક પાસેથી અમારા વિશે સાંભળ્યું હતું"
		}
		if label != "" {
			return fmt.Sprintf("%s પર અમારી ad જોઈને enquiry કરી હતી", label)
		}
	case "pa":
		switch source {
		case "website":
			return "ਸਾਡੀ ਵੈੱਬਸਾਈਟ 'ਤੇ ਫਾਰਮ ਭਰਿਆ ਸੀ"
		case "referral":
			return "ਸਾਡੇ ਕਿਸੇ ਗਾਹਕ ਤੋਂ ਸਾਡੇ ਬਾਰੇ ਸੁਣਿਆ ਸੀ"
		}
		if label != "" {
			return fmt.Sprintf("%s 'ਤੇ ਸਾਡਾ ad ਵੇਖ ਕੇ enquiry ਕੀਤੀ ਸੀ", label)
		}
	case "ta":
		switch source {
		case "website":
			return "எங்கள் வலைத்தளத்தில் படிவம் நிரப்பியிருந்தீர்கள்"
		case "referral":
			return "எங்கள் ஒரு வாடிக்கையாளரிடம் எங்களைப் பற்றி கேட்டிருந்தீர்கள்"
		}
		if label != "" {
			return fmt.Sprintf("%s-இல் எங்கள் ad பார்த்து enquiry செய்திருந்தீர்கள்", label)
		}
	case "te":
		switch source {
		case "website":
			return "మా వెబ్‌సైట్‌లో ఫారమ్ నింపారు"
		case "referral":
			return "మా కస్టమర్‌ల నుండి మా గురించి విన్నారు"
		}
		if label != "" {
			return fmt.Sprintf("%s లో మా ad చూసి enquiry చేశారు", label)
		}
	case "kn":
		switch source {
		case "website":
			return "ನಮ್ಮ ವೆಬ್‌ಸೈಟ್‌ನಲ್ಲಿ ಫಾರ್ಮ್ ಭರ್ತಿ ಮಾಡಿದ್ದೀರಿ"
		case "referral":
			return "ನಮ್ಮ ಗ್ರಾಹಕರಿಂದ ನಮ್ಮ ಬಗ್ಗೆ ಕೇಳಿದ್ದೀರಿ"
		}
		if label != "" {
			return fmt.Sprintf("%s ನಲ್ಲಿ ನಮ್ಮ ad ನೋಡಿ enquiry ಮಾಡಿದ್ದೀರಿ", label)
		}
	case "ml":
		switch source {
		case "website":
			return "ഞങ്ങളുടെ വെബ്സൈറ്റിൽ ഫോം പൂരിപ്പിച്ചിരുന്നു"
		case "referral":
			return "ഞങ്ങളുടെ ഒരു ഉപഭോക്താവിൽ നിന്ന് ഞങ്ങളെപ്പറ്റി കേട്ടിരുന്നു"
		}
		if label != "" {
			return fmt.Sprintf("%s-ൽ ഞങ്ങളുടെ ad കണ്ട് enquiry ചെയ്തിരുന്നു", label)
		}
	default: // English and any other language
		switch source {
		case "website":
			return "fill out the form on our website"
		case "referral":
			return "hear about us from one of our customers"
		}
		if label != "" {
			return fmt.Sprintf("see our ad on %s and enquire", label)
		}
	}
	return ""
}

// buildGreeting composes the opening line the TTS speaks. Every language uses
// the same structure: (1) salutation + lead name, (2) agent introduction
// including company name and a language-appropriate speaking verb (bol), and
// (3) an inline question acknowledging the lead source. Each piece gracefully
// degrades when the corresponding input is missing (no name / no agent /
// no source).
func buildGreeting(leadName, companyName, agentName, bol, source, language string) string {
	firstName := firstWord(leadName)
	company := coalesce(companyName, "our company")
	inline := sourceContextInline(source, language)

	t := greetingTemplates[language]
	if t.salutation == "" {
		t = greetingTemplates["en"]
	}

	var b strings.Builder
	if firstName != "" {
		b.WriteString(fmt.Sprintf(t.salutationWithName, firstName))
	} else {
		b.WriteString(t.salutation)
	}
	b.WriteString(t.punct)
	b.WriteByte(' ')

	if agentName != "" {
		b.WriteString(fmt.Sprintf(t.intro, agentName, company, bol))
	} else {
		b.WriteString(fmt.Sprintf(t.introNoAgent, company, bol))
	}
	b.WriteString(t.punct)

	if inline != "" {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf(t.question, inline))
		b.WriteString(t.questionMark)
	}
	return b.String()
}

// greetingTemplate captures the per-language shape of a greeting. Verb suffixes
// and punctuation differ enough that a tiny template per language is clearer
// than deeply nested switches.
type greetingTemplate struct {
	salutationWithName string // "%s" = first name
	salutation         string // used when lead has no name
	punct              string // sentence terminator used twice (after salutation, after intro)
	intro              string // "%s" = agent name, "%s" = company, "%s" = bol
	introNoAgent       string // "%s" = company, "%s" = bol (used when voice ID is unset)
	question           string // "%s" = inline source phrase
	questionMark       string // language-appropriate question mark
}

var greetingTemplates = map[string]greetingTemplate{
	"hi": {
		salutationWithName: "नमस्ते %s जी",
		salutation:         "नमस्ते",
		punct:              "।",
		intro:              "मैं %s, %s से %s",
		introNoAgent:       "मैं %s से %s",
		question:           "क्या आपने %s",
		questionMark:       "?",
	},
	"mr": {
		salutationWithName: "नमस्कार %s जी",
		salutation:         "नमस्कार",
		punct:              ".",
		intro:              "मी %s, %s कडून %s",
		introNoAgent:       "मी %s कडून %s",
		question:           "तुम्ही %s का",
		questionMark:       "?",
	},
	"bn": {
		salutationWithName: "নমস্কার %s জি",
		salutation:         "নমস্কার",
		punct:              "।",
		intro:              "আমি %s, %s থেকে %s",
		introNoAgent:       "আমি %s থেকে %s",
		question:           "আপনি %s কি",
		questionMark:       "?",
	},
	"gu": {
		salutationWithName: "નમસ્તે %s જી",
		salutation:         "નમસ્તે",
		punct:              ".",
		intro:              "હું %s, %s થી %s",
		introNoAgent:       "હું %s થી %s",
		question:           "શું તમે %s",
		questionMark:       "?",
	},
	"pa": {
		salutationWithName: "ਸਤ ਸ੍ਰੀ ਅਕਾਲ %s ਜੀ",
		salutation:         "ਸਤ ਸ੍ਰੀ ਅਕਾਲ",
		punct:              "।",
		intro:              "ਮੈਂ %s, %s ਤੋਂ %s",
		introNoAgent:       "ਮੈਂ %s ਤੋਂ %s",
		question:           "ਕੀ ਤੁਸੀਂ %s",
		questionMark:       "?",
	},
	"ta": {
		salutationWithName: "வணக்கம் %s",
		salutation:         "வணக்கம்",
		punct:              ".",
		intro:              "நான் %s, %s-லிருந்து %s",
		introNoAgent:       "நான் %s-லிருந்து %s",
		question:           "நீங்கள் %s",
		questionMark:       "?",
	},
	"te": {
		salutationWithName: "నమస్కారం %s గారు",
		salutation:         "నమస్కారం",
		punct:              ".",
		intro:              "నేను %s, %s నుండి %s",
		introNoAgent:       "నేను %s నుండి %s",
		question:           "మీరు %s",
		questionMark:       "?",
	},
	"kn": {
		salutationWithName: "ನಮಸ್ಕಾರ %s ಜೀ",
		salutation:         "ನಮಸ್ಕಾರ",
		punct:              ".",
		intro:              "ನಾನು %s, %s ಇಂದ %s",
		introNoAgent:       "ನಾನು %s ಇಂದ %s",
		question:           "ನೀವು %s",
		questionMark:       "?",
	},
	"ml": {
		salutationWithName: "നമസ്കാരം %s",
		salutation:         "നമസ്കാരം",
		punct:              ".",
		intro:              "ഞാൻ %s, %s-ൽ നിന്ന് %s",
		introNoAgent:       "ഞാൻ %s-ൽ നിന്ന് %s",
		question:           "നിങ്ങൾ %s",
		questionMark:       "?",
	},
	"en": {
		salutationWithName: "Hi %s",
		salutation:         "Hello",
		punct:              ".",
		// Positional verbs reorder (agent, company, bol) into natural English.
		intro:        "I'm %[1]s %[3]s from %[2]s",
		introNoAgent: "I'm %[2]s from %[1]s",
		question:     "Did you %s",
		questionMark: "?",
	},
}

// resolveSource picks the lead's source if it's a known dropdown value, else the
// campaign's source. Unknown values (e.g., "Manual" from Quick-Add) are ignored.
// Aliases are normalised so downstream helpers see canonical keys.
func resolveSource(leadSource, campaignSource string) string {
	if s := canonicalSource(leadSource); s != "" {
		return s
	}
	return canonicalSource(campaignSource)
}

func canonicalSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "meta", "fb":
		return "facebook"
	case "insta":
		return "instagram"
	case "google ads":
		return "google"
	case "facebook", "google", "instagram", "linkedin", "website", "referral", "cold":
		return s
	}
	return ""
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
