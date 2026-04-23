package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/globussoft/callified-backend/internal/callguard"
)

// ── GET /api/tasks ───────────────────────────────────────────────────────────

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

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.CompleteTask(id); err != nil {
		s.logger.Sugar().Errorw("completeTask", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"completed": true})
}

// ── GET /api/reports ─────────────────────────────────────────────────────────

func (s *Server) getReports(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) addPronunciation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Word     string `json:"word"`
		Phonetic string `json:"phonetic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" || body.Phonetic == "" {
		writeError(w, http.StatusBadRequest, "word and phonetic required")
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

// ── GET /api/recordings/{filename} ───────────────────────────────────────────
// Serves stereo WAV recordings from the recordings directory.
// Auth-gated so recordings are not publicly accessible.

func (s *Server) serveRecording(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")

	// Reject path traversal: no slashes, no ".." segments
	if strings.ContainsAny(filename, "/\\") || strings.Contains(filename, "..") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	fullPath := filepath.Join(s.cfg.RecordingsDir, filename)
	http.ServeFile(w, r, fullPath)
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

	leads, _ := s.db.GetAllLeads(ac.OrgID)
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
