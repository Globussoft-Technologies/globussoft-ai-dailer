package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/globussoft/callified-backend/internal/callguard"
	"github.com/globussoft/callified-backend/internal/db"
)

// Pronunciation guide values are concatenated into the LLM system prompt and
// rendered in admin/CSV surfaces, so they must be a strict allow-list to block
// stored prompt-injection and XSS (issue #81). Letters, digits, space, hyphen,
// apostrophe, period only; 1–50 chars; must contain at least one alphanumeric.
var pronAllowed = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 .'\-]{0,49}$`)

// sanitizeEmailForPath converts an email into a safe directory name.
func sanitizeEmailForPath(email string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(email) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// ── GET /api/tasks ───────────────────────────────────────────────────────────

// @Summary     List tasks
// @Description Returns all CRM follow-up tasks for the org.
// @Tags        misc
// @Produce     json
// @Security    BearerAuth
// @Success     200  {array}   db.Task
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/tasks [get]
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	tasks, err := s.db.GetAllTasks(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listTasks", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(tasks))
}

// ── PUT /api/tasks/{id}/complete ─────────────────────────────────────────────

// @Summary     Complete task
// @Description Marks a CRM follow-up task as completed.
// @Tags        misc
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Task ID"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/tasks/{id}/complete [put]
func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.CompleteTask(id, ac.OrgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		s.logger.Sugar().Errorw("completeTask", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"completed": true})
}

// ── GET /api/reports ─────────────────────────────────────────────────────────

// @Summary     Get reports
// @Description Returns org-level aggregate reports. Requires Admin role.
// @Tags        misc
// @Produce     json
// @Security    BearerAuth
// @Success     200  {object}  db.Report
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/reports [get]
func (s *Server) getReports(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "reports.view") {
		return
	}
	ac := getAuth(r)
	report, err := s.db.GetReports(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("getReports", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── GET /api/pronunciation ───────────────────────────────────────────────────

// @Summary     List pronunciations
// @Description Returns the org-wide pronunciation guide used by the AI agent. Requires Admin role.
// @Tags        misc
// @Produce     json
// @Security    BearerAuth
// @Success     200  {array}   db.Pronunciation
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/pronunciation [get]
func (s *Server) listPronunciations(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.GetAllPronunciations()
	if err != nil {
		s.logger.Sugar().Errorw("listPronunciations", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(list))
}

// ── POST /api/pronunciation ──────────────────────────────────────────────────

// @Summary     Add pronunciation
// @Description Adds or updates a word-to-phonetic mapping in the pronunciation guide. Requires Admin role.
// @Tags        misc
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body  body      object{word=string,phonetic=string}  true  "Word and phonetic spelling"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/pronunciation [post]
func (s *Server) addPronunciation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Word     string `json:"word"`
		Phonetic string `json:"phonetic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" || body.Phonetic == "" {
		writeError(w, http.StatusBadRequest, "word and phonetic required")
		return
	}
	body.Word = strings.TrimSpace(body.Word)
	body.Phonetic = strings.TrimSpace(body.Phonetic)
	if !pronAllowed.MatchString(body.Word) {
		writeError(w, http.StatusBadRequest, "written word: only letters, digits, spaces, hyphens, apostrophes, periods (max 50 chars)")
		return
	}
	if !pronAllowed.MatchString(body.Phonetic) {
		writeError(w, http.StatusBadRequest, "how to pronounce: only letters, digits, spaces, hyphens, apostrophes, periods (max 50 chars)")
		return
	}
	if strings.EqualFold(body.Word, body.Phonetic) {
		writeError(w, http.StatusBadRequest, "phonetic must differ from word")
		return
	}
	if err := s.db.UpsertPronunciation(body.Word, body.Phonetic); err != nil {
		s.logger.Sugar().Errorw("addPronunciation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── DELETE /api/pronunciation/{id} ───────────────────────────────────────────

// @Summary     Delete pronunciation
// @Description Removes a pronunciation rule. Requires Admin role.
// @Tags        misc
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Pronunciation ID"
// @Success     200  {object}  DeletedResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/pronunciation/{id} [delete]
func (s *Server) deletePronunciation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := s.db.DeletePronunciation(id)
	if err != nil {
		s.logger.Sugar().Errorw("deletePronunciation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "pronunciation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/recordings/{filepath} ───────────────────────────────────────────
// Serves stereo WAV recordings from the recordings directory.
// Auth-gated so recordings are not publicly accessible.
// Supports both legacy flat URLs (/api/recordings/filename.wav) and the new
// per-user segregated URLs (/api/recordings/user_email/filename.wav).

// @Summary     Serve recording
// @Description Streams a call recording WAV file. Auth-gated.
// @Tags        recordings
// @Produce     audio/wav
// @Security    BearerAuth
// @Param       filename  path  string  true  "Recording filename"
// @Success     200  {file}    binary
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Router      /api/recordings/{filename} [get]
func (s *Server) serveRecording(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.recordings") {
		return
	}
	ac := getAuth(r)
	relPath := r.PathValue("filename")

	// Reject path traversal: no ".." segments anywhere
	if strings.Contains(relPath, "..") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	// Clean the path and ensure it stays under the recordings directory.
	relPath = filepath.Clean("/" + relPath)
	if relPath == "/" {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	relPath = relPath[1:] // strip leading slash added for Clean

	// Authorise: the local recording URL stored in call_transcripts must belong
	// to the caller's org. Cloud recordings are served directly from object
	// storage and do not hit this endpoint.
	recordingURL := "/api/recordings/" + relPath
	tx, err := s.db.GetTranscriptByRecordingURL(ac.OrgID, recordingURL)
	if err != nil {
		s.logger.Sugar().Errorw("serveRecording", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tx == nil {
		writeError(w, http.StatusNotFound, "recording not found")
		return
	}

	fullPath := filepath.Join(s.cfg.RecordingsDir, relPath)
	// Backward compatibility: if the segregated path doesn't exist, fall back
	// to the legacy flat location in the recordings root.
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		legacyPath := filepath.Join(s.cfg.RecordingsDir, filepath.Base(relPath))
		if _, err2 := os.Stat(legacyPath); err2 == nil {
			fullPath = legacyPath
		}
	}
	http.ServeFile(w, r, fullPath)
}

// ── POST /api/upload-recording ───────────────────────────────────────────────
//
// Accepts the browser-side MediaRecorder upload (Opus-in-webm, captured at the
// AudioContext's native rate — typically 48kHz). The server-side stereo WAV
// we already save is 8kHz telephony audio and sounds muffled; the webm
// recording is noticeably clearer. Ported from Python routes.py
// api_upload_recording — the frontend has always been uploading this, but Go
// was missing the handler (404 → file lost → user only has the 8kHz WAV to
// play back, which is what "recording not clear" was actually about).
//
// After saving the file we replace the transcript row's recording_url with
// the webm URL so the UI plays the higher-quality version. Polls briefly
// because finalizeCall runs in a goroutine — the transcript row may not
// exist yet when the browser POSTs the file.

// @Summary     Upload browser recording
// @Description Accepts a browser MediaRecorder webm upload and attaches it to the latest transcript.
// @Tags        recordings
// @Accept      multipart/form-data
// @Produce     json
// @Security    BearerAuth
// @Param       file     formData  file    true   "Opus/webm audio file"
// @Param       lead_id  formData  string  false  "Lead ID (optional, used to link recording)"
// @Success     200  {object}  object{status=string,url=string}
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     503  {object}  ErrorResponse
// @Router      /api/upload-recording [post]
func (s *Server) uploadRecording(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.recordings") {
		return
	}
	if s.cfg.RecordingsDir == "" {
		writeError(w, http.StatusServiceUnavailable, "recordings dir not configured")
		return
	}
	// Room for ~5 minutes of Opus at 128kbps ≈ 5MB; 20MB is generous.
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "parse form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	leadIDStr := r.FormValue("lead_id")
	streamSid := strings.TrimSpace(r.FormValue("stream_sid"))
	if len(streamSid) > 255 {
		writeError(w, http.StatusBadRequest, "stream_sid too long")
		return
	}

	// Prefer client-provided filename; fall back to synthesised name.
	fname := filepath.Base(header.Filename)
	if fname == "" || fname == "." || fname == "/" {
		fname = fmt.Sprintf("call_%s_%d.webm", leadIDStr, time.Now().UnixMilli())
	}
	// Defence: strip any path traversal — only the basename survives.
	fname = filepath.Base(fname)

	data, readErr := io.ReadAll(file)
	if readErr != nil {
		s.logger.Sugar().Errorw("uploadRecording: read", "err", readErr)
		writeError(w, http.StatusInternalServerError, "read failed")
		return
	}

	ac := getAuth(r)

	// Validate lead access before touching any transcript rows.
	if leadID, convErr := strconv.ParseInt(leadIDStr, 10, 64); convErr == nil && leadID > 0 {
		if !s.canAccessLead(ac, leadID) {
			writeError(w, http.StatusNotFound, "lead not found")
			return
		}
	}

	userDir := ""
	if ac.Email != "" {
		userDir = sanitizeEmailForPath(ac.Email)
	}

	// Try to determine the campaign from the lead's latest transcript so the
	// webm recording can be grouped under recordings/<email>/<campaign>/.
	campaignDir := ""
	campaignID := int64(0)
	if cid, convErr := strconv.ParseInt(r.FormValue("campaign_id"), 10, 64); convErr == nil && cid > 0 {
		campaignID = cid
	}
	if leadID, convErr := strconv.ParseInt(leadIDStr, 10, 64); convErr == nil && leadID > 0 {
		if txs, err := s.db.GetTranscriptsByLead(leadID); err == nil && len(txs) > 0 {
			if campaignID == 0 {
				campaignID = txs[0].CampaignID
			}
			if c, err := s.db.GetCampaignByID(campaignID); err == nil && c != nil {
				campaignDir = sanitizeEmailForPath(c.Name)
			}
		}
	}

	objectKey := "recordings/" + fname
	if userDir != "" {
		objectKey = "recordings/" + userDir + "/" + fname
		if campaignDir != "" {
			objectKey = "recordings/" + userDir + "/" + campaignDir + "/" + fname
		}
	}

	var recURL string
	// OCI takes precedence when configured.
	if s.oci != nil {
		publicURL, err := s.oci.UploadPublic(r.Context(), objectKey, data)
		if err != nil {
			s.logger.Sugar().Warnw("uploadRecording: OCI upload failed", "err", err)
			// Fall through to S3 or local save.
		} else {
			recURL = publicURL
			s.logger.Sugar().Infow("uploadRecording: uploaded to OCI", "url", publicURL, "lead_id", leadIDStr)
		}
	}

	if recURL == "" && s.s3 != nil {
		publicURL, err := s.s3.UploadPublic(r.Context(), objectKey, data)
		if err != nil {
			s.logger.Sugar().Warnw("uploadRecording: S3 upload failed", "err", err)
			// Fall through to local save.
		} else {
			recURL = publicURL
			s.logger.Sugar().Infow("uploadRecording: uploaded to S3", "url", publicURL, "lead_id", leadIDStr)
		}
	}

	if recURL == "" {
		baseDir := s.cfg.RecordingsDir
		urlPrefix := "/api/recordings/"
		if userDir != "" {
			baseDir = filepath.Join(baseDir, userDir)
			urlPrefix = "/api/recordings/" + userDir + "/"
			if campaignDir != "" {
				baseDir = filepath.Join(baseDir, campaignDir)
				urlPrefix = urlPrefix + campaignDir + "/"
			}
		}
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			s.logger.Sugar().Errorw("uploadRecording: mkdir", "err", err)
			writeError(w, http.StatusInternalServerError, "mkdir failed")
			return
		}
		fpath := filepath.Join(baseDir, fname)
		if err := os.WriteFile(fpath, data, 0644); err != nil {
			s.logger.Sugar().Errorw("uploadRecording: write", "err", err, "path", fpath)
			writeError(w, http.StatusInternalServerError, "write failed")
			return
		}
		recURL = urlPrefix + fname
		s.logger.Sugar().Infow("uploadRecording: saved locally", "path", fpath, "bytes", len(data), "lead_id", leadIDStr)
	}

	// Swap the stereo-WAV URL on the most recent transcript for this lead
	// to point at the higher-quality webm instead. Poll up to ~5s because
	// the transcript row is inserted asynchronously by finalizeCall —
	// matches the Python handler's retry loop.
	if leadID, convErr := strconv.ParseInt(leadIDStr, 10, 64); convErr == nil && leadID > 0 {
		s.attachRecordingToLatestTranscript(r.Context(), leadID, campaignID, ac.OrgID, streamSid, recURL)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "url": recURL})
}

// attachRecordingToLatestTranscript finds the most recent transcript for
// leadID and fills in recording_url ONLY IF it's still empty. Mirrors Python
// routes.py:1181-1190 — the server-side stereo WAV (saved by finalizeCall) is
// the canonical recording, so we only let the browser webm "win" when the WAV
// path produced nothing. Without this guard the webm overwrites a perfectly
// good 8kHz stereo mix and the modal renders "Browser Recording" instead of
// "Server Recording (Stereo)".
//
// Polls because finalizeCall runs in a goroutine — the transcript row may not
// exist yet when the browser POSTs the file.
func (s *Server) attachRecordingToLatestTranscript(ctx context.Context, leadID, campaignID, orgID int64, streamSid, recURL string) {
	since := time.Now().Add(-5 * time.Minute)
	// Wait longer for browser web-sim calls because finalizeCall (which creates
	// the transcript row and server-side WAV) may still be draining the WS and
	// saving the recording. Previously we only polled 3s, so the browser upload
	// frequently won the race and created an empty transcript row.
	const maxAttempts = 10 // 5 seconds; finalizeCall should create the row quickly for web-sim
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var latest *db.Transcript
		var err error
		if streamSid != "" {
			latest, err = s.db.GetTranscriptByCallSid(streamSid)
		} else {
			latest, err = s.db.GetRecentTranscriptForRecordingAttach(leadID, campaignID, since)
		}
		if err == nil && latest != nil {
			if latest.RecordingURL != "" {
				s.logger.Sugar().Infow("uploadRecording: server recording already attached, skipping webm",
					"transcript_id", latest.ID, "existing", latest.RecordingURL)
				return
			}
			if err := s.db.UpdateCallTranscriptRecording(latest.ID, recURL); err != nil {
				s.logger.Sugar().Warnw("uploadRecording: update transcript url failed",
					"transcript_id", latest.ID, "err", err)
			} else {
				s.logger.Sugar().Infow("uploadRecording: transcript url updated (no server WAV present)",
					"transcript_id", latest.ID, "url", recURL)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	s.logger.Sugar().Warnw("uploadRecording: no transcript row found after waiting, creating fallback empty row",
		"lead_id", leadID, "campaign_id", campaignID, "url", recURL)
	// No transcript row exists (e.g. web-sim with no server-side WAV, or
	// finalizeCall didn't run). Create an empty row carrying the webm URL
	// so the call still appears in the Transcripts modal as audio-only.
	transcriptID, err := s.db.SaveCallTranscriptWithCallSid(leadID, campaignID, orgID, streamSid, "[]", recURL, "", 0)
	if err != nil {
		s.logger.Sugar().Warnw("uploadRecording: no transcript and create failed",
			"lead_id", leadID, "url", recURL, "err", err)
		return
	}
	s.logger.Sugar().Infow("uploadRecording: created empty transcript with webm",
		"transcript_id", transcriptID, "lead_id", leadID, "url", recURL)
}

// ── GET /ping ─────────────────────────────────────────────────────────────────
// No-auth health ping for UptimeRobot / load-balancer health checks.

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── GET /api/debug/health ─────────────────────────────────────────────────────

func (s *Server) debugHealth(w http.ResponseWriter, r *http.Request) {
	result := map[string]string{"status": "ok"}
	if err := s.db.Ping(); err != nil {
		result["db"] = "error: " + err.Error()
		result["status"] = "degraded"
	} else {
		result["db"] = "ok"
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /api/calling-status ───────────────────────────────────────────────────

func (s *Server) callingStatus(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	tz, err := s.db.GetOrgTimezone(ac.OrgID)
	if err != nil {
		tz = "Asia/Kolkata"
	}
	status := callguard.Check(tz)
	writeJSON(w, http.StatusOK, status)
}

// ── GET /api/onboarding ───────────────────────────────────────────────────────

func (s *Server) getOnboarding(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	completed, err := s.db.IsOnboardingCompleted(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("getOnboarding", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"completed": completed})
}

// ── GET /api/onboarding/status ───────────────────────────────────────────────
// Full status response matching the Python API (completed + step flags).

func (s *Server) onboardingStatus(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	completed, _ := s.db.IsOnboardingCompleted(ac.OrgID)

	leads, _ := s.db.GetAllLeads(ac.OrgID, nil, false)
	campaigns, _ := s.db.GetCampaignsByOrg(ac.OrgID)
	vs, _ := s.db.GetOrganizationVoiceSettings(ac.OrgID)

	writeJSON(w, http.StatusOK, map[string]any{
		"completed": completed,
		"steps": map[string]bool{
			"leads":    len(leads) > 0,
			"voice":    vs.TTSVoiceID != "",
			"campaign": len(campaigns) > 0,
		},
	})
}

// ── POST /api/onboarding/complete ────────────────────────────────────────────

func (s *Server) completeOnboarding(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if err := s.db.MarkOnboardingCompleted(ac.OrgID); err != nil {
		s.logger.Sugar().Errorw("completeOnboarding", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"completed": true})
}

// ── GET /api/demo-requests ────────────────────────────────────────────────────

func (s *Server) listDemoRequests(w http.ResponseWriter, r *http.Request) {
	reqs, err := s.db.GetAllDemoRequests()
	if err != nil {
		s.logger.Sugar().Errorw("listDemoRequests", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(reqs))
}

// ── POST /api/demo-requests ───────────────────────────────────────────────────

func (s *Server) createDemoRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
		Company string `json:"company"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Email == "" {
		writeError(w, http.StatusBadRequest, "name and email required")
		return
	}
	id, err := s.db.CreateDemoRequest(body.Name, body.Email, body.Phone, body.Company, body.Message)
	if err != nil {
		s.logger.Sugar().Errorw("createDemoRequest", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── GET /api/whatsapp ─────────────────────────────────────────────────────────

func (s *Server) listWhatsappLogs(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	logs, err := s.db.GetAllWhatsappLogs(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listWhatsappLogs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(logs))
}

// ── GET /api/debug/logs ───────────────────────────────────────────────────────
// Returns recent entries from the callified:live-logs Redis list.

func (s *Server) debugLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if v, err := strconv.Atoi(nStr); err == nil && v > 0 {
			n = v
		}
	}
	ctx := context.Background()
	logs, err := s.store.GetLiveLogs(ctx, n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "redis error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "count": len(logs)})
}

// ── POST /api/test-email ──────────────────────────────────────────────────────

func (s *Server) testEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.To == "" {
		writeError(w, http.StatusBadRequest, "to required")
		return
	}
	if body.Subject == "" {
		body.Subject = "Test Email from Callified AI"
	}
	if body.Body == "" {
		body.Body = "<p>This is a test email from Callified AI.</p>"
	}
	if err := s.emailSvc.Send(body.To, body.Subject, body.Body); err != nil {
		writeError(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

// ── GET /api/debug/last-dial ──────────────────────────────────────────────────
// Returns metadata about the most recent dial attempt.

func (s *Server) debugLastDial(w http.ResponseWriter, r *http.Request) {
	cl, err := s.db.GetLastDialMeta()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"last_dial": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"last_dial": cl})
}

// ── GET /api/debug/call-timeline ─────────────────────────────────────────────
// Returns the most recent call transcripts for the org.

func (s *Server) debugCallTimeline(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	timeline, err := s.db.GetRecentCallTimeline(ac.OrgID, 20)
	if err != nil {
		s.logger.Sugar().Errorw("debugCallTimeline", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(timeline))
}

// ── GET /api/receptionist/calls ──────────────────────────────────────────────
// Returns post-call inbound receptionist rows: customer name, phone, transcript
// and recording URL after extraction has created or matched a CRM lead.

func (s *Server) listReceptionistCalls(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	calls, err := s.db.GetRecentInboundReceptionistCalls(ac.OrgID, 50)
	if err != nil {
		s.logger.Sugar().Errorw("listReceptionistCalls", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for i := range calls {
		changed := fillReceptionistCallFromTranscript(&calls[i])
		if changed {
			_ = s.db.UpdateCallTranscriptInboundDetails(
				calls[i].TranscriptID,
				calls[i].FirstName,
				calls[i].LastName,
				calls[i].Phone,
				calls[i].Interest,
				calls[i].Status,
			)
		}
	}
	writeJSON(w, http.StatusOK, emptyJSON(calls))
}

type persistedReceptionistTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func fillReceptionistCallFromTranscript(call *db.InboundReceptionistCall) bool {
	if call == nil || len(call.Transcript) == 0 {
		return false
	}
	originalName := strings.TrimSpace(call.FirstName + " " + call.LastName)
	originalPhone := call.Phone
	var turns []persistedReceptionistTurn
	if err := json.Unmarshal(call.Transcript, &turns); err != nil {
		return false
	}
	for _, turn := range turns {
		if !strings.EqualFold(turn.Role, "User") && !strings.EqualFold(turn.Role, "Customer") {
			continue
		}
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		if call.Phone == "" {
			if phone := extractReceptionistPhone(text); phone != "" {
				call.Phone = phone
			}
		}
		if name := extractReceptionistName(text); name != "" {
			call.FirstName = name
			call.LastName = ""
		}
	}
	return originalName != strings.TrimSpace(call.FirstName+" "+call.LastName) || originalPhone != call.Phone
}

func extractReceptionistPhone(text string) string {
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

func extractReceptionistName(text string) string {
	lower := strings.ToLower(text)
	for _, prefix := range []string{
		"sorry, this is ",
		"sorry this is ",
		"my name is ",
		"this is ",
		"i am ",
		"i'm ",
		"name is ",
	} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(text[idx+len(prefix):])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], ".,!?;:\"'()[]{}")
		if name != "" && receptionistNameHasLetter(name) {
			return name
		}
	}
	return ""
}

func receptionistNameHasLetter(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

type receptionistCallUpdateRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Interest  string `json:"interest"`
	Status    string `json:"status"`
}

func (s *Server) updateReceptionistCall(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	transcriptID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	call, err := s.db.GetReceptionistCallByTranscript(ac.OrgID, transcriptID)
	if err != nil {
		s.logger.Sugar().Errorw("updateReceptionistCall: fetch", "err", err, "transcript_id", transcriptID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if call == nil {
		writeError(w, http.StatusNotFound, "call not found")
		return
	}
	var req receptionistCallUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	first := strings.TrimSpace(req.FirstName)
	last := strings.TrimSpace(req.LastName)
	phone := normalizePhone(strings.TrimSpace(req.Phone))
	interest := strings.TrimSpace(req.Interest)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "new"
	}
	if err := s.db.UpdateCallTranscriptInboundDetails(transcriptID, first, last, phone, interest, status); err != nil {
		s.logger.Sugar().Errorw("updateReceptionistCall: update transcript fields", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	leadID := call.LeadID
	if leadID == 0 {
		if first == "" && last == "" && phone == "" && interest == "" {
			writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
			return
		}
		id, err := s.db.CreateLead(first, last, phone, "Inbound Call", interest, "", 0, ac.OrgID)
		if err != nil {
			if isDuplicateEntryError(err) && phone != "" {
				existing, findErr := s.db.GetLeadByPhoneOrg(phone, ac.OrgID, nil, false)
				if findErr != nil || existing == nil {
					writeFieldError(w, http.StatusConflict, "phone number already exists", map[string]string{"phone": "Phone number already exists"})
					return
				}
				leadID = existing.ID
			} else {
				s.logger.Sugar().Errorw("updateReceptionistCall: create lead", "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		} else {
			leadID = id
		}
		if err := s.db.UpdateCallTranscriptLead(transcriptID, leadID); err != nil {
			s.logger.Sugar().Errorw("updateReceptionistCall: attach lead", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	updated, err := s.db.UpdateLead(leadID, first, last, phone, "Inbound Call", interest, "", 0, ac.OrgID)
	if err != nil {
		if isDuplicateEntryError(err) {
			writeFieldError(w, http.StatusConflict, "phone number already exists", map[string]string{"phone": "Phone number already exists for another lead"})
			return
		}
		s.logger.Sugar().Errorw("updateReceptionistCall: update lead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !updated {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	_ = s.db.UpdateLeadDisposition(leadID, status, "", "")
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "lead_id": leadID})
}

// ── GET /api/debug/recording-config ──────────────────────────────────────────
// Reports whether the post-call WAV pipeline is wired correctly. Mostly a
// diagnostic for the empty-`recording_url` case where saveWAV silently
// returns "" because RECORDINGS_DIR is unset, the volume isn't mounted, or
// the directory isn't writable. Probes the runtime state directly — env
// var, stat result, write probe, file count — so a single curl reveals
// which of the four likely causes is in play. Admin-gated.
func (s *Server) debugRecordingConfig(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"recordings_dir":     s.cfg.RecordingsDir,
		"recordings_dir_env": os.Getenv("RECORDINGS_DIR"),
		"recording_svc":      s.recordingSvcName(),
	}

	dir := s.cfg.RecordingsDir
	if dir == "" {
		out["status"] = "unconfigured"
		out["reason"] = "cfg.RecordingsDir is empty — saveWAV returns \"\" silently"
		writeJSON(w, http.StatusOK, out)
		return
	}

	info, err := os.Stat(dir)
	if err != nil {
		out["status"] = "missing"
		out["reason"] = fmt.Sprintf("stat %s: %v", dir, err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["dir_exists"] = true
	out["is_dir"] = info.IsDir()
	out["mode"] = info.Mode().String()

	// Write probe: try creating a tiny temp file to confirm the dir is
	// writable by the audiod uid. Cleaned up immediately on success.
	probe := filepath.Join(dir, fmt.Sprintf(".rwprobe-%d", time.Now().UnixNano()))
	if err := os.WriteFile(probe, []byte("ok"), 0644); err != nil {
		out["writable"] = false
		out["write_error"] = err.Error()
	} else {
		out["writable"] = true
		_ = os.Remove(probe)
	}

	// Count existing WAV files. A non-zero count means recordings ARE being
	// written — in that case the bug is in the call_transcripts.recording_url
	// linkage, not the WAV save.
	entries, err := os.ReadDir(dir)
	if err != nil {
		out["read_error"] = err.Error()
	} else {
		wavCount := 0
		recent := make([]map[string]any, 0, 5)
		// Iterate newest-last; we'll surface the last 5.
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".wav") {
				continue
			}
			wavCount++
			fi, err := e.Info()
			if err != nil {
				continue
			}
			if len(recent) < 5 || fi.ModTime().After(time.Time{}) {
				recent = append(recent, map[string]any{
					"name":     e.Name(),
					"size":     fi.Size(),
					"mod_time": fi.ModTime().Format(time.RFC3339),
				})
			}
		}
		out["wav_count"] = wavCount
		// Last few, newest first by mod_time. Best-effort — entries from
		// ReadDir aren't sorted by time, so the slice may be a sample, not
		// strictly the newest. Still useful as a sanity check.
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		out["recent_wavs"] = recent
	}

	out["status"] = "ok"
	writeJSON(w, http.StatusOK, out)
}

// recordingSvcName returns a one-word indicator of the recording-service
// hookup so the debug endpoint can flag the case where the wshandler was
// constructed without a recordingSvc (post-call save is then a no-op).
func (s *Server) recordingSvcName() string {
	if s == nil || s.cfg == nil {
		return "unknown"
	}
	if s.cfg.RecordingsDir == "" {
		return "no-dir"
	}
	return "wired"
}

// ── Public trial signup ───────────────────────────────────────────────────────
//
// POST /api/public/trial-signup creates a fully functional trial account from
// the marketing website form. It provisions an org, an admin user, a 7-day
// admin subscription, and 100 minutes of prepaid calling credit.

const (
	trialMinutes     = 100
	trialExpiryDays  = 7
	trialPasswordLen = 10
)

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func generateRandomPassword(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}

func setTrialCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

type trialSignupRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

func (s *Server) trialSignup(w http.ResponseWriter, r *http.Request) {
	setTrialCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var req trialSignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)

	if req.FirstName == "" || req.Email == "" || req.Phone == "" {
		writeError(w, http.StatusBadRequest, "first name, email and phone are required")
		return
	}
	if !emailRegex.MatchString(req.Email) {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	// Prevent duplicate signups.
	existing, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("trialSignup: GetUserByEmail", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	password, err := generateRandomPassword(trialPasswordLen)
	if err != nil {
		s.logger.Sugar().Errorw("trialSignup: generate password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	orgName := fmt.Sprintf("%s's Organization", req.FirstName)
	orgID, err := s.db.CreateOrganization(orgName)
	if err != nil {
		s.logger.Sugar().Errorw("trialSignup: CreateOrganization", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	hash, err := db.HashPassword(password)
	if err != nil {
		s.logger.Sugar().Errorw("trialSignup: HashPassword", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	fullName := strings.TrimSpace(req.FirstName + " " + req.LastName)
	_, err = s.db.CreateUser(req.Email, hash, fullName, "Admin", orgID)
	if err != nil {
		s.logger.Sugar().Errorw("trialSignup: CreateUser", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	expiresAt := time.Now().UTC().AddDate(0, 0, trialExpiryDays)
	if _, err := s.db.CreateAdminSubscription(req.Email, expiresAt, "trial"); err != nil {
		s.logger.Sugar().Errorw("trialSignup: CreateAdminSubscription", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	deltaPaise := int64(trialMinutes) * int64(db.DefaultRatePerMinPaise)
	if _, err := s.db.AddCredits(orgID, deltaPaise, "trial_bonus", "trial-signup", fmt.Sprintf("%d free trial minutes", trialMinutes)); err != nil {
		s.logger.Sugar().Errorw("trialSignup: AddCredits", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Keep a record in the demo-requests table for sales follow-up.
	if _, err := s.db.CreateDemoRequest(fullName, req.Email, req.Phone, "", "Trial signup"); err != nil {
		s.logger.Sugar().Warnw("trialSignup: CreateDemoRequest", "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"provisioned": true,
		"credentials": map[string]any{
			"username":  req.Email,
			"password":  password,
			"login_url": "https://app.callified.ai",
		},
	})
}
