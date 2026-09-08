// Package recording handles end-of-call processing: WAV saving, Gemini analysis,
// call review insertion, DND auto-add, and webhook + WA confirmation dispatch.
// This replaces the gRPC FinalizeCall Python call (Phase 4).
package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/wa"
	"github.com/globussoft/callified-backend/internal/webhook"
)

// SaveRequest contains all data needed to save and analyze one call.
type SaveRequest struct {
	StreamSid   string
	CallSid     string
	LeadID      int64
	CampaignID  int64
	OrgID       int64
	LeadPhone   string
	AgentName   string
	TTSLanguage string // language the call was synthesised in (hi/mr/bn/gu/pa/ta/te/kn/ml/en)
	ChatHistory []llm.ChatMessage
	DurationS   float32
	StereoWav   []byte // nil → no server-side recording
	// SkipCredits=true: do not deduct this call from the org's prepaid balance.
	SkipCredits bool
	// UserEmail is the agent/admin who initiated the call; recordings are saved
	// under a per-user subfolder.
	UserEmail string
	// IsInbound means the call started without a lead; post-call analysis should
	// extract customer details and attach the transcript to a lead when possible.
	IsInbound bool
}

// Service handles post-call analysis.
// uploader is a minimal interface so the recording package doesn't need to
// import the storage package directly (avoids any future cycle).
type uploader interface {
	UploadPublic(ctx context.Context, key string, data []byte) (string, error)
}

type Service struct {
	database   *db.DB
	llm        *llm.Provider
	dispatcher *webhook.Dispatcher
	cfg        *config.Config
	log        *zap.Logger
	s3         uploader // nil when S3 is not configured
	oci        uploader // nil when OCI is not configured; takes precedence over S3
}

// New creates a Service.
func New(database *db.DB, llmProvider *llm.Provider, dispatcher *webhook.Dispatcher, cfg *config.Config, log *zap.Logger) *Service {
	return &Service{
		database:   database,
		llm:        llmProvider,
		dispatcher: dispatcher,
		cfg:        cfg,
		log:        log,
	}
}

// SetS3Uploader wires in an S3 client after construction.
func (s *Service) SetS3Uploader(u uploader) { s.s3 = u }

// SetOCIUploader wires in an OCI Object Storage client after construction.
// When set, OCI takes precedence over S3 for recording uploads.
func (s *Service) SetOCIUploader(u uploader) { s.oci = u }

// SaveAndAnalyze runs the full post-call pipeline asynchronously.
// It is fire-and-forget from the WebSocket handler's perspective — call it in a goroutine.
func (s *Service) SaveAndAnalyze(ctx context.Context, req SaveRequest) {
	// Use a background context so cleanup isn't cancelled when the WS connection closes.
	ctx = context.Background()

	// Resolve campaign name for the recording subfolder.
	campaignName := ""
	if req.CampaignID > 0 && s.database != nil {
		if c, err := s.database.GetCampaignByID(req.CampaignID); err == nil && c != nil {
			campaignName = c.Name
		}
	}

	// 1. Save WAV to disk (if recorded server-side).
	recordingURL := ""
	if len(req.StereoWav) > 0 {
		recordingURL = s.saveWAV(req.StreamSid, req.UserEmail, campaignName, req.StereoWav)
	}

	// 2. Build transcript turns ([{role,text}, ...]) from chat history.
	//    Mirrors recording_service.py: role mapping model→AI / user→User,
	//    empty-text turns dropped.
	transcriptJSON, turnCount := historyToTranscript(req.ChatHistory)

	// 3. Skip only when there are no turns AND no recording — i.e. truly empty
	//    sessions (immediate disconnect with no audio). When a recording exists
	//    we still persist the row so the call shows up in the Transcripts modal
	//    and the WebM-upload path has a row to attach its URL to. Without this,
	//    calls with audio but no STT/LLM turns silently disappeared from the UI.
	// Browser web-sim calls are always persisted: the browser uploads the
	// actual recording separately and the row must exist for that attachment.
	isWebSim := strings.HasPrefix(req.StreamSid, "web_sim_")
	if turnCount == 0 && recordingURL == "" && !isWebSim {
		s.log.Info("recording: skipping empty transcript",
			zap.String("stream_sid", req.StreamSid),
			zap.Int("raw_turns", len(req.ChatHistory)))
		return
	}
	if isWebSim && turnCount == 0 {
		s.log.Warn("recording: web-sim call has empty chat history",
			zap.String("stream_sid", req.StreamSid),
			zap.Int64("lead_id", req.LeadID),
			zap.Int("raw_turns", len(req.ChatHistory)))
	}

	// 4. Persist transcript row. For browser web-sim calls the browser upload
	// often creates an empty placeholder row before we get here (race). Check
	// for a recent placeholder row for this lead+campaign and populate it
	// instead of creating a second, duplicate row.
	var transcriptID int64
	callSid := strings.TrimSpace(req.CallSid)
	if callSid == "" {
		callSid = strings.TrimSpace(req.StreamSid)
	}
	var placeholder *db.Transcript
	if callSid != "" {
		placeholder, _ = s.database.GetTranscriptByCallSid(callSid)
	}
	if placeholder == nil && callSid == "" {
		placeholder, _ = s.database.GetRecentTranscriptForRecordingAttach(req.LeadID, req.CampaignID, time.Now().Add(-5*time.Minute))
	}
	if placeholder != nil && string(placeholder.Transcript) == "[]" {
		if err := s.database.UpdateCallTranscriptContents(placeholder.ID, transcriptJSON, req.DurationS, req.TTSLanguage); err != nil {
			s.log.Error("recording: failed to populate placeholder transcript", zap.Int64("transcript_id", placeholder.ID), zap.Error(err))
			// Fall through to insert a fresh row.
		} else {
			transcriptID = placeholder.ID
			// If finalizeCall produced a server-side WAV and the placeholder only
			// has the browser webm, overwrite with the higher-quality WAV.
			if recordingURL != "" && placeholder.RecordingURL != recordingURL {
				if err := s.database.UpdateCallTranscriptRecording(placeholder.ID, recordingURL); err != nil {
					s.log.Warn("recording: failed to update placeholder recording URL", zap.Int64("transcript_id", placeholder.ID), zap.Error(err))
				}
			}
			s.log.Info("recording: populated placeholder transcript",
				zap.Int64("transcript_id", transcriptID),
				zap.Int("turn_count", turnCount),
				zap.Float32("duration_s", req.DurationS))
		}
	}
	if transcriptID == 0 {
		var err error
		transcriptID, err = s.database.SaveCallTranscriptWithCallSid(req.LeadID, req.CampaignID, req.OrgID, callSid, transcriptJSON, recordingURL, req.TTSLanguage, req.DurationS)
		if err != nil {
			s.log.Error("recording: SaveCallTranscript failed", zap.Error(err))
			return
		}
		s.log.Info("recording: transcript saved",
			zap.Int64("transcript_id", transcriptID),
			zap.Int("turn_count", turnCount),
			zap.Float32("duration_s", req.DurationS))
	}

	if req.IsInbound {
		if err := s.database.UpdateCallTranscriptDirection(transcriptID, "inbound"); err != nil {
			s.log.Warn("recording: mark inbound transcript failed", zap.Int64("transcript_id", transcriptID), zap.Error(err))
		}
	}

	if req.IsInbound && req.LeadID == 0 && s.llm != nil && len(req.ChatHistory) > 0 {
		if leadID, phone, err := s.upsertInboundLead(ctx, req.OrgID, transcriptID, req.ChatHistory, req.LeadPhone); err != nil {
			s.log.Warn("recording: inbound lead extraction failed", zap.Error(err))
		} else if leadID > 0 {
			req.LeadID = leadID
			if phone != "" {
				req.LeadPhone = phone
			}
			s.log.Info("recording: inbound transcript attached to lead",
				zap.Int64("transcript_id", transcriptID),
				zap.Int64("lead_id", leadID))
		}
	}

	// 4. Run Gemini analysis (non-critical — log and continue on failure).
	//
	// Skip analysis entirely for very short / one-sided calls: a 2-3 second
	// hang-up or a no-reply greeting has nothing meaningful to analyze, and
	// asking Gemini to score it tends to produce inflated 4/5 + positive
	// because Gemini judges the agent's intro instead of the call outcome.
	// Skipping means no call_reviews row is saved, so the UI's conclusion
	// card naturally stays hidden for these calls.
	userTurns := 0
	for _, m := range req.ChatHistory {
		if strings.EqualFold(m.Role, "user") {
			userTurns++
		}
	}
	shouldAnalyze := s.llm != nil && len(req.ChatHistory) > 0 && req.DurationS >= 10 && userTurns >= 1

	review := &db.CallReview{
		TranscriptID: transcriptID,
		OrgID:        req.OrgID,
		LeadID:       req.LeadID,
		Sentiment:    "neutral",
	}
	analyzed := false
	if shouldAnalyze {
		if a, err := s.analyzeCall(ctx, req.ChatHistory); err != nil {
			s.log.Warn("recording: Gemini analysis failed", zap.Error(err))
		} else {
			review.QualityScore = a.QualityScore
			review.Sentiment = a.Sentiment
			review.AppointmentBooked = a.AppointmentBooked
			review.FailureReason = a.FailureReason
			review.WhatWentWell = a.WhatWentWell
			review.WhatWentWrong = a.WhatWentWrong
			review.Summary = a.Summary
			review.Insights = a.Insights
			review.PromptImprovementSuggestion = a.PromptImprovementSuggestion
			analyzed = true
		}
	} else {
		s.log.Info("recording: skipping Gemini analysis (short/one-sided)",
			zap.Int64("transcript_id", transcriptID),
			zap.Float32("duration_s", req.DurationS),
			zap.Int("user_turns", userTurns))
	}

	// 5. Save call review only when Gemini actually produced commentary.
	// Saving an empty-default row would render in the UI as a misleading
	// "0/5 neutral / no appointment" card with no real conclusion.
	if analyzed {
		if err := s.database.SaveCallReview(review); err != nil {
			s.log.Error("recording: SaveCallReview failed", zap.Error(err))
		}
	}

	// 5b. Deduct call duration from the org's prepaid credit balance.
	// Skipped automatically for web-sim calls (CallSid == "") so only real
	// telephony calls are billed. Also skipped when SkipCredits is set
	// (unlimited manual calls for AI-hidden users). Idempotent on the call_sid
	// — if the recording pipeline runs twice for the same call (race between
	// Exotel's "completed" callback and the WS-side finalize), the second call
	// is a no-op.
	if req.CallSid != "" && req.DurationS > 0 && req.OrgID > 0 && !req.SkipCredits {
		if charge, balance, err := s.database.DeductCallCredits(req.OrgID, req.CallSid, float64(req.DurationS)); err != nil {
			s.log.Warn("recording: DeductCallCredits failed",
				zap.String("call_sid", req.CallSid), zap.Error(err))
		} else if charge > 0 {
			s.log.Info("recording: credits deducted",
				zap.String("call_sid", req.CallSid),
				zap.Int64("charge_paise", charge),
				zap.Int64("balance_after_paise", balance),
				zap.Float32("duration_s", req.DurationS))
		}
	} else if req.SkipCredits {
		s.log.Info("recording: skipping credit deduction for unlimited manual call",
			zap.String("call_sid", req.CallSid),
			zap.Int64("org_id", req.OrgID),
			zap.Float32("duration_s", req.DurationS))
	}

	// 6. Auto-DND if clearly negative + "do not call" intent.
	if review.Sentiment == "negative" && req.LeadPhone != "" && containsDNC(req.ChatHistory) {
		if err := s.database.AddDNDNumber(req.OrgID, req.LeadPhone, "auto: negative sentiment + DNC intent"); err != nil {
			s.log.Warn("recording: auto-DND failed", zap.Error(err))
		} else {
			s.log.Info("recording: auto-added to DND", zap.String("phone", req.LeadPhone))
		}
	}

	// 7. Fire call.completed webhook.
	if s.dispatcher != nil {
		s.dispatcher.Dispatch(ctx, req.OrgID, "call.completed", map[string]any{
			"transcript_id":      transcriptID,
			"lead_id":            req.LeadID,
			"campaign_id":        req.CampaignID,
			"duration_s":         req.DurationS,
			"sentiment":          review.Sentiment,
			"appointment_booked": review.AppointmentBooked,
		})
	}

	// 8. Send WA appointment confirmation if appointment was booked.
	if review.AppointmentBooked && req.LeadPhone != "" {
		s.sendAppointmentConfirmation(ctx, req.OrgID, req.LeadPhone, req.AgentName)
	}

	s.log.Info("recording: post-call processing complete",
		zap.Int64("transcript_id", transcriptID),
		zap.String("sentiment", review.Sentiment),
		zap.Bool("appointment_booked", review.AppointmentBooked),
	)
}

// ── WAV saving ────────────────────────────────────────────────────────────────

func (s *Service) saveWAV(streamSid, userEmail, campaignName string, data []byte) string {
	filename := fmt.Sprintf("%s_%d.wav", sanitize(streamSid), time.Now().UnixMilli())

	// Build the object key once; OCI and S3 use the same key layout.
	userDir := ""
	if userEmail != "" {
		userDir = sanitizeForPath(userEmail)
	}
	campaignDir := ""
	if campaignName != "" {
		campaignDir = sanitizeForPath(campaignName)
	}
	objectKey := "recordings/" + filename
	if userDir != "" {
		objectKey = "recordings/" + userDir + "/" + filename
		if campaignDir != "" {
			objectKey = "recordings/" + userDir + "/" + campaignDir + "/" + filename
		}
	}

	// OCI takes precedence when configured.
	if s.oci != nil {
		publicURL, err := s.oci.UploadPublic(context.Background(), objectKey, data)
		if err != nil {
			s.log.Warn("recording: OCI upload failed", zap.Error(err))
			// Fall through to S3 or local save.
		} else {
			s.log.Info("recording: uploaded to OCI", zap.String("url", publicURL))
			return publicURL
		}
	}

	if s.s3 != nil {
		publicURL, err := s.s3.UploadPublic(context.Background(), objectKey, data)
		if err != nil {
			s.log.Warn("recording: S3 upload failed", zap.Error(err))
			// Fall through to local save.
		} else {
			s.log.Info("recording: uploaded to S3", zap.String("url", publicURL))
			return publicURL
		}
	}

	if s.cfg.RecordingsDir == "" {
		return ""
	}

	// Segregate recordings into per-user folders when the initiating agent's
	// email is known. Fall back to the root recordings directory otherwise.
	baseDir := s.cfg.RecordingsDir
	urlPrefix := "/api/recordings/"
	if userEmail != "" {
		userDir := sanitizeForPath(userEmail)
		baseDir = filepath.Join(baseDir, userDir)
		urlPrefix = "/api/recordings/" + userDir + "/"
		if campaignName != "" {
			campaignDir := sanitizeForPath(campaignName)
			baseDir = filepath.Join(baseDir, campaignDir)
			urlPrefix = urlPrefix + campaignDir + "/"
		}
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		s.log.Warn("recording: mkdir failed", zap.Error(err))
		return ""
	}
	path := filepath.Join(baseDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		s.log.Warn("recording: WriteFile failed", zap.Error(err))
		return ""
	}
	return urlPrefix + filename
}

// ── Gemini analysis ───────────────────────────────────────────────────────────

// Analysis is the exported view of the LLM scoring output. Used by the API
// layer's on-demand conclusion endpoint without importing internal types.
type Analysis = analysis

type analysis struct {
	QualityScore                float64 `json:"quality_score"`
	Sentiment                   string  `json:"sentiment"`
	AppointmentBooked           bool    `json:"appointment_booked"`
	FailureReason               string  `json:"failure_reason"`
	WhatWentWell                string  `json:"what_went_well"`
	WhatWentWrong               string  `json:"what_went_wrong"`
	Summary                     string  `json:"summary"`
	Insights                    string  `json:"insights"`
	PromptImprovementSuggestion string  `json:"prompt_improvement_suggestion"`
}

// AnalyzeCall is the public wrapper around analyzeCall. Used by the API
// layer to (re)generate a call conclusion on demand when the post-call
// pipeline skipped it (short/one-sided call) or the operator hits a
// "Regenerate" button. Keeps the same prompt and parsing as the post-call
// path so the conclusion card stays consistent regardless of who triggered it.
func (s *Service) AnalyzeCall(ctx context.Context, history []llm.ChatMessage) (*Analysis, error) {
	return s.analyzeCall(ctx, history)
}

// Strict prompt: every prose field MUST be populated (never empty, never
// omitted), score MUST be an integer 1-5, and the analysis is always written
// in English regardless of the transcript language.
//
// Crucially, the score must reflect CALL OUTCOME (did the customer engage and
// move toward an appointment?), not just agent technique. A 3-second one-sided
// greeting where the customer never spoke is a FAILED call and must score
// 1-2 with neutral sentiment — regardless of how well the AI delivered its
// intro. Previously Gemini was scoring these 4/5 + positive because it judged
// the agent's performance instead of the call outcome.
const analysisSystemPrompt = `You are a sales call quality analyst scoring CALL OUTCOME, not agent technique. Analyze the provided transcript and return ONLY a JSON object with EVERY field populated in English (no nulls, no empty strings, no omitted keys):

SCORING RULES — apply STRICTLY:
- If the customer never spoke (0 user turns / only the AI delivered a greeting): "quality_score" must be 1 or 2, "sentiment" must be "neutral", "appointment_booked" must be false. The customer's silence is the call outcome — never call this "positive" no matter how well the agent introduced themselves.
- If the customer spoke only 1-2 short words ("hello", "yes", "ok") and the call ended: score 2. Sentiment "neutral".
- If the customer engaged but refused or pushed back: score 2-3. Sentiment "negative" or "annoyed" depending on tone.
- If the customer engaged and the conversation progressed but no booking happened: score 3-4. Sentiment based on customer's actual tone.
- Only score 5 when an appointment was booked AND the customer sounded positive.

FIELDS:
- "quality_score": integer 1-5 only (NEVER outside 1-5)
- "sentiment": "positive" | "neutral" | "negative" | "annoyed" — measure the CUSTOMER's tone, not the agent's
- "appointment_booked": true or false (true only if a specific date/time was confirmed)
- "failure_reason": 1 sentence in English on why the call didn't convert; if it did, write "N/A — appointment booked". For no-reply calls, write e.g. "Customer did not respond after greeting — likely hung up or wrong number"
- "what_went_well": 1-2 sentences in English on what the agent did right. If nothing meaningful happened (no reply), say "Agent delivered greeting clearly but had no chance to engage the customer"
- "what_went_wrong": 1-2 sentences on what the agent could improve. For no-reply calls, say "No opportunity to engage — call ended before any customer interaction"
- "summary": 1-2 sentence summary referencing what specifically happened in THIS transcript. Record only requirements the customer clearly stated in direct response to a question. Ignore background voices, other people near the phone, TV/radio, and one-off remarks unrelated to the conversation — never present them as the customer's interest or requirement
- "insights": 1 coaching insight in English for next time
- "prompt_improvement_suggestion": 1 specific, actionable instruction to add to the AI system prompt to improve future calls of this kind

The transcript may be in any language (Telugu, Hindi, English, etc.); ALWAYS write your analysis fields in English regardless of the transcript language. Reference specific things from THIS transcript — never write generic filler.

Return ONLY valid JSON. No markdown, no explanation. Keep each string under 240 chars.`

func (s *Service) analyzeCall(ctx context.Context, history []llm.ChatMessage) (*analysis, error) {
	transcript := formatTranscript(history)
	userMsg := llm.ChatMessage{Role: "user", Text: "Analyze this call transcript:\n\n" + transcript}

	// 1500 tokens is enough for the 8-key JSON object including 200-char
	// strings each. The previous 512 cap truncated mid-key, causing every
	// post-call analysis to fail JSON parsing → all reviews saved with
	// quality_score=0, sentiment="neutral" defaults. Issue: empty insight
	// columns in the Call Insights tab.
	raw, err := s.llm.GenerateResponse(ctx, analysisSystemPrompt, []llm.ChatMessage{userMsg}, 1500)
	if err != nil {
		return nil, err
	}

	// Strip markdown fences if Gemini wraps in ```json ... ```
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = raw[strings.Index(raw, "\n")+1:]
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}

	var a analysis
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, fmt.Errorf("analysis JSON parse: %w (raw: %s)", err, raw[:min(len(raw), 200)])
	}
	// Clamp quality_score to the 0-5 scale the UI expects.
	if a.QualityScore < 0 {
		a.QualityScore = 0
	}
	if a.QualityScore > 5 {
		a.QualityScore = 5
	}
	if a.Sentiment == "" {
		a.Sentiment = "neutral"
	}
	return &a, nil
}

type inboundLeadExtraction struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Phone      string `json:"phone"`
	Interest   string `json:"interest"`
	Company    string `json:"company"`
	Status     string `json:"status"`
	FollowNote string `json:"follow_up_note"`
}

const inboundLeadExtractionPrompt = `Extract CRM lead details from an inbound receptionist call.
Return ONLY a valid JSON object with these exact keys:
- "first_name": string
- "last_name": string
- "phone": string, preferably E.164 if clearly available, otherwise the exact spoken number
- "interest": short customer requirement
- "company": customer company if mentioned, else empty
- "status": one of "new", "Qualified", "Appointment Booked", "Not Interested"
- "follow_up_note": one concise CRM note
If a field was not provided, use an empty string. Do not invent details.`

func (s *Service) upsertInboundLead(ctx context.Context, orgID, transcriptID int64, history []llm.ChatMessage, fallbackPhone string) (int64, string, error) {
	raw, err := s.llm.GenerateResponse(ctx, inboundLeadExtractionPrompt, []llm.ChatMessage{{
		Role: "user",
		Text: "Transcript:\n\n" + formatTranscript(history),
	}}, 900)
	if err != nil {
		return 0, "", err
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = raw[strings.Index(raw, "\n")+1:]
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	var ex inboundLeadExtraction
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		return 0, "", fmt.Errorf("inbound extraction JSON parse: %w", err)
	}
	ex.FirstName = strings.TrimSpace(ex.FirstName)
	ex.LastName = strings.TrimSpace(ex.LastName)
	ex.Phone = strings.TrimSpace(ex.Phone)
	fallbackPhone = strings.TrimSpace(fallbackPhone)
	ex.Interest = strings.TrimSpace(ex.Interest)
	ex.Company = strings.TrimSpace(ex.Company)
	ex.Status = strings.TrimSpace(ex.Status)
	ex.FollowNote = strings.TrimSpace(ex.FollowNote)
	applyInboundTranscriptFallback(&ex, history)
	if ex.Phone == "" {
		ex.Phone = fallbackPhone
	}
	if ex.Status == "" {
		ex.Status = "new"
	}
	if err := s.database.UpdateCallTranscriptInboundDetails(transcriptID, ex.FirstName, ex.LastName, ex.Phone, ex.Interest, ex.Status); err != nil {
		s.log.Warn("recording: save inbound transcript details failed", zap.Int64("transcript_id", transcriptID), zap.Error(err))
	}

	var leadID int64
	if ex.Phone != "" {
		if existing, err := s.database.GetLeadByPhoneOrg(ex.Phone, orgID, nil, false); err != nil {
			return 0, ex.Phone, err
		} else if existing != nil {
			leadID = existing.ID
			first := coalesceString(ex.FirstName, existing.FirstName)
			last := coalesceString(ex.LastName, existing.LastName)
			interest := coalesceString(ex.Interest, existing.Interest)
			company := coalesceString(ex.Company, existing.Company)
			if _, err := s.database.UpdateLead(leadID, first, last, existing.Phone, "Inbound Call", interest, company, existing.ExecutiveID, orgID); err != nil {
				return 0, ex.Phone, err
			}
		}
	}
	if leadID == 0 && (ex.FirstName != "" || ex.LastName != "" || ex.Phone != "" || ex.Interest != "" || ex.Company != "") {
		id, err := s.database.CreateLead(ex.FirstName, ex.LastName, ex.Phone, "Inbound Call", ex.Interest, ex.Company, 0, orgID)
		if err != nil {
			s.log.Warn("recording: inbound lead create failed, keeping transcript leadless", zap.Error(err))
			return 0, ex.Phone, nil
		}
		leadID = id
	}
	if leadID > 0 {
		_ = s.database.UpdateLeadDisposition(leadID, ex.Status, ex.FollowNote, "")
		_ = s.database.UpdateCallTranscriptLead(transcriptID, leadID)
	}
	return leadID, ex.Phone, nil
}

func applyInboundTranscriptFallback(ex *inboundLeadExtraction, history []llm.ChatMessage) {
	for _, turn := range history {
		if turn.Role != "user" {
			continue
		}
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		if ex.Phone == "" {
			if phone := extractPhoneDigits(text); phone != "" {
				ex.Phone = phone
			}
		}
		if name := extractSpokenName(text); name != "" {
			// Later corrections like "Sorry, this is Sri" should win over an
			// earlier misheard name.
			ex.FirstName = name
			ex.LastName = ""
		}
	}
}

func extractPhoneDigits(text string) string {
	var digits strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	raw := digits.String()
	if strings.HasPrefix(raw, "91") && len(raw) == 12 {
		raw = raw[2:]
	}
	if strings.HasPrefix(raw, "0") && len(raw) > 10 {
		raw = strings.TrimPrefix(raw, "0")
	}
	if len(raw) == 10 {
		return raw
	}
	return ""
}

func extractSpokenName(text string) string {
	lower := strings.ToLower(text)
	prefixes := []string{
		"sorry, this is ",
		"sorry this is ",
		"my name is ",
		"this is ",
		"i am ",
		"i'm ",
		"name is ",
	}
	for _, prefix := range prefixes {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(text[idx+len(prefix):])
		if rest == "" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], ".,!?;:\"'()[]{}")
		if name != "" && containsASCIILetter(name) {
			return name
		}
	}
	return ""
}

func containsASCIILetter(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

func coalesceString(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── WA appointment confirmation ───────────────────────────────────────────────

func (s *Service) sendAppointmentConfirmation(ctx context.Context, orgID int64, phone, agentName string) {
	channels, err := s.database.GetWAChannelConfigsByOrg(orgID)
	if err != nil || len(channels) == 0 {
		return
	}
	ch := channels[0]
	cfg := wa.ChannelConfig{
		Provider:    ch.Provider,
		PhoneNumber: ch.PhoneNumber,
		APIKey:      ch.APIKey,
		AppID:       ch.AppID,
	}
	msg := fmt.Sprintf("Hi! Your appointment has been confirmed. Our representative %s will be in touch shortly.", agentName)
	if err := wa.SendText(ctx, cfg, phone, msg); err != nil {
		s.log.Warn("recording: WA appointment confirmation failed", zap.Error(err))
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// historyToTranscript builds the persisted transcript turns in the shape the
// frontend reads and Python's recording_service produces:
//
//	[{"role":"AI","text":"..."}, {"role":"User","text":"..."}]
//
// Role mapping follows the Python code exactly (recording_service.py:38):
//   - internal "model" → "AI"   (agent bubble in TranscriptModal)
//   - everything else  → "User" (customer bubble)
//
// Empty-text turns are skipped — matching Python's `if text:` guard — so a
// row is never saved for a "connected but nothing said" call.
//
// Returns (json_string, turn_count). The caller checks turn_count to decide
// whether to persist (Python: `if transcript_turns: save_call_transcript(...)`).
func historyToTranscript(history []llm.ChatMessage) (string, int) {
	type persistedTurn struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	out := make([]persistedTurn, 0, len(history))
	for _, m := range history {
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		role := "User"
		if m.Role == "model" {
			role = "AI"
		}
		out = append(out, persistedTurn{Role: role, Text: text})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]", 0
	}
	return string(b), len(out)
}

func formatTranscript(history []llm.ChatMessage) string {
	var sb strings.Builder
	for _, m := range history {
		role := "Agent"
		if m.Role == "user" {
			role = "Customer"
		}
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(m.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

var dncKeywords = []string{"do not call", "don't call", "stop calling", "remove me", "not interested", "blocked"}

func containsDNC(history []llm.ChatMessage) bool {
	for _, m := range history {
		if m.Role != "user" {
			continue
		}
		lower := strings.ToLower(m.Text)
		for _, kw := range dncKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// sanitizeForPath turns an email into a safe directory name.
func sanitizeForPath(s string) string {
	return sanitize(strings.ToLower(s))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
