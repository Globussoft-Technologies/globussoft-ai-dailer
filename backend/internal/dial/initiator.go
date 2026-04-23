package dial

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/callguard"
	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/webhook"
)

// CallData holds the information needed to initiate one outbound call.
type CallData struct {
	LeadID      int64
	LeadName    string
	LeadPhone   string
	CampaignID  int64
	OrgID       int64
	Interest    string
	Language    string
	TTSProvider string
	TTSVoiceID  string
	TTSLanguage string
}

// Initiator orchestrates the full dial sequence:
// DND check → TRAI hours → Redis pending call → provider dial → DB log.
type Initiator struct {
	cfg     *config.Config
	store   *rstore.Store
	db      *db.DB
	disp    *webhook.Dispatcher
	twilio  *TwilioClient
	exotel  *ExotelClient
	log     *zap.Logger
}

// New creates an Initiator wired to both telephony providers.
func New(cfg *config.Config, store *rstore.Store, database *db.DB, disp *webhook.Dispatcher, log *zap.Logger) *Initiator {
	return &Initiator{
		cfg:    cfg,
		store:  store,
		db:     database,
		disp:   disp,
		twilio: NewTwilioClient(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioPhone),
		exotel: NewExotelClient(cfg.ExotelAPIKey, cfg.ExotelAPIToken, cfg.ExotelAccountSID, cfg.ExotelCallerID, cfg.ExotelAppID),
		log:    log,
	}
}

// ErrDND is returned when the lead is on the DND list.
var ErrDND = fmt.Errorf("lead is on DND list")

// ErrCallHours is returned when TRAI calling hours are not active.
var ErrCallHours = fmt.Errorf("outside TRAI calling hours (9 AM – 9 PM)")

// Initiate performs the full dial sequence for one lead.
// Returns nil on successful dial initiation (not call completion).
func (i *Initiator) Initiate(ctx context.Context, data CallData) error {
	// 1. DND check
	isDND, err := i.db.IsDNDNumber(data.OrgID, data.LeadPhone)
	if err != nil {
		i.log.Warn("dial: DND check failed", zap.Error(err))
	}
	if isDND {
		_ = i.db.UpdateLeadStatus(data.LeadID, "DND — do not call")
		return ErrDND
	}

	// 2. TRAI calling hours
	tz, _ := i.db.GetOrgTimezone(data.OrgID)
	status := callguard.Check(tz)
	if !status.Allowed {
		return fmt.Errorf("%w: %s", ErrCallHours, status.Reason)
	}

	// 3. Store pending call info in Redis (wshandler reads this on stream connect)
	pending := rstore.PendingCallInfo{
		Name:        data.LeadName,
		Phone:       data.LeadPhone,
		LeadID:      data.LeadID,
		Interest:    data.Interest,
		CampaignID:  data.CampaignID,
		TTSProvider: data.TTSProvider,
		TTSVoiceID:  data.TTSVoiceID,
		TTSLanguage: data.TTSLanguage,
	}

	// 4. Dial via the configured provider
	provider := i.cfg.DefaultProvider
	var callSid string

	switch provider {
	case "twilio":
		twimlURL := fmt.Sprintf("%s/webhook/twilio?lead_id=%d&campaign_id=%d",
			i.cfg.PublicServerURL, data.LeadID, data.CampaignID)
		statusURL := fmt.Sprintf("%s/webhook/twilio/status", i.cfg.PublicServerURL)
		callSid, err = i.twilio.InitiateCall(ctx, data.LeadPhone, twimlURL, statusURL)
	default: // exotel
		statusURL := fmt.Sprintf("%s/webhook/exotel/status?lead_id=%d&campaign_id=%d",
			i.cfg.PublicServerURL, data.LeadID, data.CampaignID)
		callSid, err = i.exotel.InitiateCall(ctx, data.LeadPhone, statusURL)
	}
	if err != nil {
		_ = i.db.UpdateLeadStatus(data.LeadID, fmt.Sprintf("Call Failed (%s)", provider))
		return fmt.Errorf("dial %s: %w", provider, err)
	}

	// 5. Persist pending call under the call SID for webhook lookup
	pending.ExotelCallSid = callSid
	if storeErr := i.store.SetPendingCall(ctx, callSid, pending); storeErr != nil {
		i.log.Warn("dial: SetPendingCall failed", zap.Error(storeErr))
	}
	// Also store under "latest" for fallback in wshandler
	_ = i.store.SetPendingCall(ctx, "latest", pending)

	// 6. Log dial attempt in DB
	if _, dbErr := i.db.SaveCallLog(data.LeadID, data.CampaignID, data.OrgID,
		callSid, provider, data.LeadPhone, "initiated"); dbErr != nil {
		i.log.Warn("dial: SaveCallLog failed", zap.Error(dbErr))
	}
	_ = i.db.IncrLeadDialAttempts(data.LeadID)
	_ = i.db.UpdateLeadStatus(data.LeadID, "Calling")

	i.log.Info("call initiated",
		zap.String("provider", provider),
		zap.String("call_sid", callSid),
		zap.Int64("lead_id", data.LeadID),
		zap.Int64("campaign_id", data.CampaignID),
	)

	// 7. Fire dial.initiated webhook
	dialData, _ := json.Marshal(map[string]any{
		"call_sid":    callSid,
		"lead_id":     data.LeadID,
		"campaign_id": data.CampaignID,
		"phone":       data.LeadPhone,
		"provider":    provider,
	})
	_ = dialData
	i.disp.Dispatch(ctx, data.OrgID, "call.initiated", map[string]any{
		"call_sid":    callSid,
		"lead_id":     data.LeadID,
		"campaign_id": data.CampaignID,
	})

	return nil
}
