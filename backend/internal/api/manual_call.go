package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/dial"
	rstore "github.com/globussoft/callified-backend/internal/redis"
)

// manualCallRequest is the JSON body accepted by POST /api/manual-call.
//
// Mode "dial" places a real outbound call via the configured carrier. Mode
// "web-sim" does not place a carrier call — it pre-registers a web-sim session
// in Redis and returns a stream_sid the caller must open /media-stream with
// (the browser sends mic PCM, the server streams TTS back).
type manualCallRequest struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Mode       string `json:"mode"` // "dial" | "web-sim"
	CampaignID int64  `json:"campaign_id"`
	Interest   string `json:"interest"`
}

// manualCall is the single REST entry point for external projects that want
// to start a call and then consume live transcripts + audio from /ws/monitor.
//
// POST /api/manual-call
//
//	Body: {"name":"Akhil","phone":"+91...","mode":"dial","campaign_id":7,"interest":"demo"}
//
//	Response (dial):     {"mode":"dial","call_sid":"...","monitor_url":"/ws/monitor/..."}
//	Response (web-sim):  {"mode":"web-sim","stream_sid":"web_sim_...","monitor_url":"...",
//	                      "media_stream_url":"/media-stream?stream_sid=..."}
//
// Connect the monitor WS to receive live events:
//
//	{"type":"transcript","role":"user|agent","text":"..."}
//	{"type":"audio","role":"user|agent","format":"pcm16_8k|ulaw_8k","payload":"<base64>"}
//
// @Summary     Manual call
// @Description Places an immediate outbound call or creates a web-sim session. Requires Admin role.
// @Tags        dialing
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body  body      manualCallRequest  true  "Call parameters. mode: 'dial' | 'web-sim'"
// @Success     200   {object}  object{mode=string,call_sid=string,monitor_url=string}
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     402   {object}  ErrorResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse
// @Failure     502   {object}  ErrorResponse
// @Router      /api/manual-call [post]
func (s *Server) manualCall(w http.ResponseWriter, r *http.Request) {
	var body manualCallRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if body.Name == "" || body.Phone == "" {
		writeError(w, http.StatusBadRequest, "name and phone required")
		return
	}

	mode := body.Mode
	if mode == "" {
		mode = "dial"
	}
	if mode != "dial" && mode != "web-sim" {
		writeError(w, http.StatusBadRequest, "mode must be 'dial' or 'web-sim'")
		return
	}
	if mode == "dial" && !s.requirePermission(w, r, "calls.dial") {
		return
	}
	if mode == "web-sim" && !s.requirePermission(w, r, "calls.browser_call") {
		return
	}

	ac := getAuth(r)
	interest := body.Interest
	if interest == "" {
		interest = "our platform"
	}

	// Pull voice settings from the campaign if one was supplied. When no
	// campaign is passed the zero-value VoiceSettings fall through and the
	// wshandler resolves org-level defaults on stream connect.
	var vs any
	if body.CampaignID > 0 {
		if !s.canViewCampaign(ac, body.CampaignID) {
			writeError(w, http.StatusNotFound, "campaign not found")
			return
		}
		vs, _ = s.db.GetCampaignVoiceSettings(body.CampaignID)
	}
	provider, voiceID, lang, maxCallDurationSeconds := extractVoice(vs)

	switch mode {
	case "dial":
		data := dial.CallData{
			LeadID:                 0,
			LeadName:               body.Name,
			LeadPhone:              body.Phone,
			CampaignID:             body.CampaignID,
			OrgID:                  ac.OrgID,
			Interest:               interest,
			TTSProvider:            provider,
			TTSVoiceID:             voiceID,
			TTSLanguage:            lang,
			MaxCallDurationSeconds: maxCallDurationSeconds,
			UserEmail:              ac.Email,
			UserID:                 userIDForDial(ac),
		}
		callSid, err := s.initiator.Initiate(r.Context(), data)
		if err != nil {
			s.logger.Warn("manualCall: initiate failed",
				zap.String("phone", body.Phone), zap.Error(err))
			writeError(w, dialErrorStatus(err), err.Error())
			return
		}
		// The monitor WS blocks up to 30s waiting for the session to register
		// under this call_sid, which covers the ringing gap between the
		// carrier accepting the API call and actually opening the media
		// stream once the callee picks up.
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":        "dial",
			"call_sid":    callSid,
			"monitor_url": fmt.Sprintf("/ws/monitor/%s", callSid),
		})

	case "web-sim":
		streamSid := fmt.Sprintf("web_sim_%d_%d", ac.OrgID, time.Now().UnixMilli())
		// Pre-register a pending call so the wshandler can hydrate the
		// session as soon as the browser opens /media-stream.
		pending := rstore.PendingCallInfo{
			Name:                   body.Name,
			Phone:                  body.Phone,
			OrgID:                  ac.OrgID,
			Interest:               interest,
			CampaignID:             body.CampaignID,
			TTSProvider:            provider,
			TTSVoiceID:             voiceID,
			TTSLanguage:            lang,
			MaxCallDurationSeconds: maxCallDurationSeconds,
			UserEmail:              ac.Email,
			UserID:                 ac.UserID,
		}
		_ = s.store.SetPendingCall(r.Context(), "latest", pending)
		_ = s.store.SetPendingCall(r.Context(), "phone:"+body.Phone, pending)

		writeJSON(w, http.StatusOK, map[string]any{
			"mode":             "web-sim",
			"stream_sid":       streamSid,
			"media_stream_url": fmt.Sprintf("/media-stream?stream_sid=%s", streamSid),
			"monitor_url":      fmt.Sprintf("/ws/monitor/%s", streamSid),
		})
	}
}

// extractVoice pulls TTS fields off the opaque vs value returned by
// db.GetCampaignVoiceSettings without creating an import cycle. Uses JSON
// round-trip rather than importing the db package's internal type.
func extractVoice(vs any) (provider, voiceID, lang string, maxCallDurationSeconds int) {
	if vs == nil {
		return
	}
	buf, err := json.Marshal(vs)
	if err != nil {
		return
	}
	var parsed struct {
		TTSProvider            string `json:"tts_provider"`
		TTSVoiceID             string `json:"tts_voice_id"`
		TTSLanguage            string `json:"tts_language"`
		MaxCallDurationSeconds int    `json:"max_call_duration_seconds"`
	}
	if err := json.Unmarshal(buf, &parsed); err != nil {
		return
	}
	return parsed.TTSProvider, parsed.TTSVoiceID, parsed.TTSLanguage, parsed.MaxCallDurationSeconds
}
