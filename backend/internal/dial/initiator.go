package dial

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/callguard"
	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/webhook"
)

// CallData holds the information needed to initiate one outbound call.
type CallData struct {
	LeadID                 int64
	LeadName               string
	LeadPhone              string
	CampaignID             int64
	OrgID                  int64
	Interest               string
	Language               string
	TTSProvider            string
	TTSVoiceID             string
	TTSLanguage            string
	MaxCallDurationSeconds int
	// IsBridge=true routes the call to browser-to-phone mode: the Exotel stream is
	// relayed to the agent's browser WebSocket instead of the AI pipeline.
	IsBridge bool
	// UserEmail identifies the agent who clicked the call button.
	UserEmail string
	// UserID is the authenticated dashboard user placing the call. When non-zero
	// and the user owns a personal provider account, the initiator prefers that
	// account over the campaign/org default so agent-initiated calls go out from
	// the agent's own credentials.
	UserID int64
	// ExotelAccountID overrides the campaign's default provider account for this
	// specific call. 0 means use the campaign default (used by AI/server calls).
	ExotelAccountID int64
}

// Initiator orchestrates the full dial sequence:
// DND check → TRAI hours → Redis pending call → provider dial → DB log.
type Initiator struct {
	cfg    *config.Config
	store  *rstore.Store
	db     *db.DB
	disp   *webhook.Dispatcher
	exotel *ExotelClient
	tata   *TataClient
	twilio *TwilioClient
	log    *zap.Logger
}

// New creates an Initiator wired to the supported telephony providers.
func New(cfg *config.Config, store *rstore.Store, database *db.DB, disp *webhook.Dispatcher, log *zap.Logger) *Initiator {
	return &Initiator{
		cfg:    cfg,
		store:  store,
		db:     database,
		disp:   disp,
		exotel: NewExotelClient(cfg.ExotelAPIKey, cfg.ExotelAPIToken, cfg.ExotelAccountSID, cfg.ExotelCallerID, cfg.ExotelAppID, "", cfg.ExotelRegion, cfg.ExotelSubdomain),
		tata:   NewTataClient(cfg.TataAPIToken, cfg.TataCallerID, cfg.TataAgentNumber, cfg.TataAPIEndpoint),
		twilio: NewTwilioClient(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioPhone),
		log:    log,
	}
}

// ErrDND is returned when the lead is on the DND list.
var ErrDND = fmt.Errorf("lead is on DND list")

// ErrCallHours is returned when TRAI calling hours are not active.
var ErrCallHours = fmt.Errorf("outside TRAI calling hours (9 AM – 9 PM)")

// ErrInsufficientCredits is returned when the org's prepaid balance is zero
// or negative. Surfaced to the API handler so it can return HTTP 402 with a
// minute-balance message instead of letting Exotel be charged for a
// dial we can't bill the customer for.
var ErrInsufficientCredits = fmt.Errorf("Credits exhausted")

// Initiate performs the full dial sequence for one lead.
// Returns the carrier-issued call SID plus nil on successful dial initiation
// (not call completion). The call_sid lets callers index the call for later
// lookup — e.g., the manual-call REST endpoint returns it so external clients
// can open /ws/monitor/{call_sid} before the media stream connects.
func (i *Initiator) Initiate(ctx context.Context, data CallData) (string, error) {
	// 1. DND check
	isDND, err := i.db.IsDNDNumber(data.OrgID, data.LeadPhone)
	if err != nil {
		i.log.Warn("dial: DND check failed", zap.Error(err))
	}
	if isDND {
		_ = i.db.UpdateLeadStatus(data.LeadID, "DND — do not call")
		// Live-feed: tell the campaign detail page why this number was skipped.
		i.store.EmitCampaignEvent(ctx, data.CampaignID, data.LeadName, data.LeadPhone, "dnd", "number is on DND list")
		return "", ErrDND
	}

	// 2. TRAI calling hours
	tz, _ := i.db.GetOrgTimezone(data.OrgID)
	status := callguard.Check(tz)
	if !status.Allowed {
		return "", fmt.Errorf("%w: %s", ErrCallHours, status.Reason)
	}

	// 2.5 Credit balance gate. Real telephony calls cost money — we won't
	// dial the provider for an org that can't pay for it. Web-sim is free
	// (it doesn't go through this Initiator at all), so the gate only
	// affects outbound Exotel/Twilio dials.
	//
	// OrgID==0 happens in a few legacy/test code paths; let those through
	// so we don't break dev environments with no billing setup.
	//
	// Every real dial path uses the same minute balance gate, so calls stop
	// once available minutes are exhausted.
	if data.OrgID > 0 {
		oc, ocErr := i.db.GetOrgCredit(data.OrgID)
		if ocErr != nil {
			i.log.Warn("dial: GetOrgCredit failed; allowing call", zap.Error(ocErr))
		} else if oc != nil && oc.BalancePaise <= 0 {
			_ = i.db.UpdateLeadStatus(data.LeadID, "Insufficient Credits")
			i.store.EmitCampaignEvent(ctx, data.CampaignID, data.LeadName, data.LeadPhone,
				"failed", ErrInsufficientCredits.Error())
			return "", ErrInsufficientCredits
		}
	}

	// 3. Store pending call info in Redis (wshandler reads this on stream connect)
	pending := rstore.PendingCallInfo{
		Name:                   data.LeadName,
		Phone:                  data.LeadPhone,
		LeadID:                 data.LeadID,
		OrgID:                  data.OrgID,
		Interest:               data.Interest,
		CampaignID:             data.CampaignID,
		TTSProvider:            data.TTSProvider,
		TTSVoiceID:             data.TTSVoiceID,
		TTSLanguage:            data.TTSLanguage,
		MaxCallDurationSeconds: data.MaxCallDurationSeconds,
		IsBridge:               data.IsBridge,
		UserEmail:              data.UserEmail,
		UserID:                 data.UserID,
	}

	// 4. Resolve provider credentials.
	// Browser calls may override the campaign default with a per-machine/org
	// account so multiple systems can dial in parallel. The override is scoped
	// to the org and validated to be a voicebot account for bridge calls.
	// When UserID is set (agent/team-leader placed the call), an explicit
	// ExotelAccountID must belong either to the org or to that user; otherwise
	// we prefer the user's personal provider account before falling back to the
	// campaign/org default.
	var creds db.ExotelCreds
	if data.ExotelAccountID > 0 {
		var lookupErr error
		if data.UserID > 0 {
			creds, lookupErr = i.db.GetOrgOrUserExotelAccountCreds(data.ExotelAccountID, data.OrgID, data.UserID)
		} else {
			creds, lookupErr = i.db.GetOrgExotelAccountCreds(data.ExotelAccountID, data.OrgID)
		}
		if lookupErr != nil {
			return "", fmt.Errorf("lookup provider account: %w", lookupErr)
		}
		if !creds.IsSet() {
			return "", fmt.Errorf("provider account not found or inaccessible")
		}
		if creds.Direction == "inbound" {
			return "", fmt.Errorf("selected provider account is inbound-only; choose an outbound account")
		}
		isTataProvider := creds.Provider == "tata" || creds.Provider == "smartflo" || creds.Provider == "tata_tele"
		if data.IsBridge && !isTataProvider && creds.AppType != "voicebot" {
			return "", fmt.Errorf("selected provider account is not a voicebot account; browser calls require app_type=voicebot")
		}
	}
	if !creds.IsSet() && data.UserID > 0 {
		if c, cerr := i.db.GetUserExotelAccountCreds(data.UserID, data.OrgID); cerr == nil && c.IsSet() {
			creds = c
		} else if cerr != nil {
			return "", fmt.Errorf("lookup user provider account: %w", cerr)
		}
	}
	if !creds.IsSet() && data.CampaignID > 0 {
		if c, cerr := i.db.GetCampaignExotelCreds(data.CampaignID); cerr == nil {
			creds = c
		}
	}
	if creds.Direction == "inbound" {
		return "", fmt.Errorf("campaign provider account is inbound-only; choose an outbound account")
	}
	provider := creds.Provider
	if provider == "" {
		provider = i.cfg.DefaultProvider
	}
	// Carry the Exotel app/flow type and account choice through to the webhook
	// and hangup path so they use the same credentials used to place the call.
	pending.AppType = creds.AppType
	if creds.AccountID > 0 {
		pending.ExotelAccountID = creds.AccountID
	}
	var callSid string

	switch provider {
	case "twilio":
		return "", fmt.Errorf("Twilio provider is disabled; choose Exotel or Tata Tele")
	case "tata", "smartflo", "tata_tele":
		var tataClient *TataClient
		if creds.IsSet() {
			tataClient = NewTataClient(creds.APIKey, creds.CallerID, creds.AppID, creds.Subdomain)
		} else {
			tataClient = i.tata
		}
		statusURL := fmt.Sprintf("%s/webhook/tata/status?lead_id=%d&campaign_id=%d",
			i.cfg.PublicServerURL, data.LeadID, data.CampaignID)
		streamURL := tataStreamURL(i.cfg.PublicServerURL, data.LeadID, data.CampaignID, data.OrgID)
		callSid, err = tataClient.InitiateCall(ctx, data.LeadPhone, statusURL, streamURL)
	default: // exotel
		if !creds.IsSet() {
			i.store.EmitCampaignEvent(ctx, data.CampaignID, data.LeadName, data.LeadPhone, "failed", "no campaign Exotel credentials set")
			return "", fmt.Errorf("no Exotel credentials configured for this campaign")
		}
		exotelClient := NewExotelClient(creds.APIKey, creds.APIToken, creds.AccountSID, creds.CallerID, creds.AppID, creds.AppType, creds.Region, creds.Subdomain)
		statusURL := fmt.Sprintf("%s/webhook/exotel/status?lead_id=%d&campaign_id=%d",
			i.cfg.PublicServerURL, data.LeadID, data.CampaignID)
		callSid, err = exotelClient.InitiateCall(ctx, data.LeadPhone, "", statusURL)
	}
	if err != nil {
		_ = i.db.UpdateLeadStatus(data.LeadID, fmt.Sprintf("Call Failed (%s)", provider))
		// Live-feed: surface the dial-time failure (bad params, provider
		// rejected, etc.) on the campaign detail page.
		i.store.EmitCampaignEvent(ctx, data.CampaignID, data.LeadName, data.LeadPhone, "failed", fmt.Sprintf("%s: %v", provider, err))
		return "", fmt.Errorf("dial %s: %w", provider, err)
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
	// Live-feed: dial went out successfully.
	i.store.EmitCampaignEvent(ctx, data.CampaignID, data.LeadName, data.LeadPhone, "dialing", fmt.Sprintf("via %s", provider))

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

	return callSid, nil
}

func tataStreamURL(publicURL string, leadID, campaignID, orgID int64) string {
	base := strings.TrimRight(publicURL, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	u, err := url.Parse(base + "/media-stream/tata")
	if err != nil {
		return base + "/media-stream/tata"
	}
	q := u.Query()
	q.Set("provider", "tata")
	if leadID > 0 {
		q.Set("lead_id", fmt.Sprint(leadID))
	}
	if campaignID > 0 {
		q.Set("campaign_id", fmt.Sprint(campaignID))
	}
	if orgID > 0 {
		q.Set("org_id", fmt.Sprint(orgID))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Hangup ends an in-progress carrier call. It first tries to use the same
// provider account that placed the call (stored in Redis under the call SID),
// then falls back to the campaign-linked account. This keeps per-machine
// browser-call overrides consistent for the whole call lifecycle.
func (i *Initiator) Hangup(ctx context.Context, callSid string, campaignID int64) error {
	if callSid == "" {
		return fmt.Errorf("missing call sid")
	}

	var creds db.ExotelCreds
	// 1. Per-call override from the Redis pending entry.
	if pending, ok := i.store.GetPendingCall(ctx, callSid); ok && pending.ExotelAccountID > 0 {
		if c, cerr := i.db.GetOrgOrUserExotelAccountCreds(pending.ExotelAccountID, pending.OrgID, 0); cerr == nil && c.IsSet() {
			creds = c
		}
	}
	// 2. Campaign default.
	if !creds.IsSet() && campaignID > 0 {
		creds, _ = i.db.GetCampaignExotelCreds(campaignID)
	}

	provider := creds.Provider
	if provider == "" {
		provider = i.cfg.DefaultProvider
	}

	switch provider {
	case "twilio":
		return fmt.Errorf("Twilio provider is disabled; choose Exotel or Tata Tele")
	case "tata", "smartflo", "tata_tele":
		var client *TataClient
		if creds.IsSet() {
			client = NewTataClient(creds.APIKey, creds.CallerID, creds.AppID, creds.Subdomain)
		} else {
			client = i.tata
		}
		return client.Hangup(ctx, callSid)
	default: // exotel
		if !creds.IsSet() {
			return fmt.Errorf("no Exotel credentials configured for this campaign")
		}
		client := NewExotelClient(creds.APIKey, creds.APIToken, creds.AccountSID, creds.CallerID, creds.AppID, creds.AppType, creds.Region, creds.Subdomain)
		return client.Hangup(ctx, callSid)
	}
}
