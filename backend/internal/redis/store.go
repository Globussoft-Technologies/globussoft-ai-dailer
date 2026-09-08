// Package redis ports redis_store.py — same key scheme so Go and Python
// can share Redis during shadow-mode parallel operation.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	keyPrefix = "callified:"
	callTTL   = time.Hour
)

// displayTZ is the timezone used for human-readable timestamps embedded in
// SSE labels (Live Campaign Activity, Live Logs). Server runs in UTC, but
// operators are in IST — formatting in the right zone here avoids
// frontend reparsing of an already-baked string. Falls back to a fixed
// +05:30 offset if Asia/Kolkata isn't in the system tzdata.
var displayTZ = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*3600+30*60)
}()

// PendingCallInfo mirrors the dict stored by python's set_pending_call().
type PendingCallInfo struct {
	Name                   string `json:"name"`
	Phone                  string `json:"phone"`
	LeadID                 int64  `json:"lead_id"`
	OrgID                  int64  `json:"org_id"`
	Interest               string `json:"interest"`
	ExotelCallSid          string `json:"exotel_call_sid"`
	CampaignID             int64  `json:"campaign_id"`
	TTSProvider            string `json:"tts_provider"`
	TTSVoiceID             string `json:"tts_voice_id"`
	TTSLanguage            string `json:"tts_language"`
	MaxCallDurationSeconds int    `json:"max_call_duration_seconds,omitempty"`
	// IsBridge=true means the call is a browser-to-phone bridge: skip AI pipeline,
	// relay audio between Exotel and the agent's browser WebSocket.
	IsBridge bool `json:"is_bridge,omitempty"`
	// AppType is the Exotel app/flow type: 'exoml' (legacy XML) or 'voicebot' (AgentStream JSON).
	AppType string `json:"app_type,omitempty"`
	// SkipCredits=true means the call should not be charged against the org's
	// prepaid balance (e.g. unlimited manual calls for AI-hidden users).
	SkipCredits bool `json:"skip_credits,omitempty"`
	// UserEmail is the agent/admin who initiated the call. Used to segregate
	// recordings into per-user folders.
	UserEmail string `json:"user_email,omitempty"`
	// UserID is the resolved DB user id of the agent/admin who initiated the call.
	UserID int64 `json:"user_id,omitempty"`
	// ExotelAccountID is the org_exotel_accounts row used to place this specific
	// call. It lets browser calls from different machines use different provider
	// accounts while the campaign default remains unchanged for AI calls.
	ExotelAccountID int64 `json:"exotel_account_id,omitempty"`
}

// Store wraps a Redis client with in-memory fallback (mirrors redis_store.py).
type Store struct {
	rdb         *goredis.Client
	log         *zap.Logger
	mu          sync.RWMutex
	memPending  map[string]PendingCallInfo
	memTakeover map[string]bool
	memWhisper  map[string][]string
}

// New creates a Store. If the Redis URL is unreachable the store silently
// falls back to in-memory maps — same behaviour as python redis_store.py.
func New(redisURL string, log *zap.Logger) *Store {
	s := &Store{
		log:         log,
		memPending:  make(map[string]PendingCallInfo),
		memTakeover: make(map[string]bool),
		memWhisper:  make(map[string][]string),
	}
	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		log.Warn("redis: invalid URL, using in-memory fallback", zap.Error(err))
		return s
	}
	opt.DialTimeout = 2 * time.Second
	s.rdb = goredis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		log.Warn("redis: ping failed, using in-memory fallback", zap.Error(err))
		s.rdb = nil
	}
	return s
}

func key(parts ...string) string { return keyPrefix + strings.Join(parts, ":") }

// ─── Pending Calls ────────────────────────────────────────────────────────────

func (s *Store) SetPendingCall(ctx context.Context, k string, info PendingCallInfo) error {
	data, _ := json.Marshal(info)
	if s.rdb != nil {
		return s.rdb.SetEx(ctx, key("pending", k), data, callTTL).Err()
	}
	s.mu.Lock()
	s.memPending[k] = info
	s.mu.Unlock()
	return nil
}

func (s *Store) GetPendingCall(ctx context.Context, k string) (PendingCallInfo, bool) {
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, key("pending", k)).Bytes()
		if err != nil {
			return PendingCallInfo{}, false
		}
		var info PendingCallInfo
		if err := json.Unmarshal(val, &info); err != nil {
			return PendingCallInfo{}, false
		}
		return info, true
	}
	s.mu.RLock()
	info, ok := s.memPending[k]
	s.mu.RUnlock()
	return info, ok
}

func (s *Store) DeletePendingCall(ctx context.Context, k string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("pending", k))
		return
	}
	s.mu.Lock()
	delete(s.memPending, k)
	s.mu.Unlock()
}

// ─── Takeover ─────────────────────────────────────────────────────────────────

func (s *Store) SetTakeover(ctx context.Context, streamSid string, active bool) error {
	val := "0"
	if active {
		val = "1"
	}
	if s.rdb != nil {
		return s.rdb.SetEx(ctx, key("takeover", streamSid), val, callTTL).Err()
	}
	s.mu.Lock()
	s.memTakeover[streamSid] = active
	s.mu.Unlock()
	return nil
}

func (s *Store) GetTakeover(ctx context.Context, streamSid string) bool {
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, key("takeover", streamSid)).Result()
		return err == nil && val == "1"
	}
	s.mu.RLock()
	v := s.memTakeover[streamSid]
	s.mu.RUnlock()
	return v
}

func (s *Store) DeleteTakeover(ctx context.Context, streamSid string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("takeover", streamSid))
		return
	}
	s.mu.Lock()
	delete(s.memTakeover, streamSid)
	s.mu.Unlock()
}

// ─── Whisper Queue ────────────────────────────────────────────────────────────

func (s *Store) PushWhisper(ctx context.Context, streamSid, message string) error {
	if s.rdb != nil {
		pipe := s.rdb.Pipeline()
		pipe.RPush(ctx, key("whisper", streamSid), message)
		pipe.Expire(ctx, key("whisper", streamSid), callTTL)
		_, err := pipe.Exec(ctx)
		return err
	}
	s.mu.Lock()
	s.memWhisper[streamSid] = append(s.memWhisper[streamSid], message)
	s.mu.Unlock()
	return nil
}

// PopAllWhispers atomically retrieves and clears the whisper queue.
// Mirrors python's redis_store.pop_all_whispers() PIPELINE(LRANGE, DEL).
func (s *Store) PopAllWhispers(ctx context.Context, streamSid string) ([]string, error) {
	if s.rdb != nil {
		k := key("whisper", streamSid)
		var msgs []string
		err := s.rdb.Watch(ctx, func(tx *goredis.Tx) error {
			vals, err := tx.LRange(ctx, k, 0, -1).Result()
			if err != nil {
				return err
			}
			msgs = vals
			_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
				pipe.Del(ctx, k)
				return nil
			})
			return err
		}, k)
		return msgs, err
	}
	s.mu.Lock()
	msgs := s.memWhisper[streamSid]
	delete(s.memWhisper, streamSid)
	s.mu.Unlock()
	return msgs, nil
}

func (s *Store) DeleteWhispers(ctx context.Context, streamSid string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("whisper", streamSid))
		return
	}
	s.mu.Lock()
	delete(s.memWhisper, streamSid)
	s.mu.Unlock()
}

// ─── Cleanup ──────────────────────────────────────────────────────────────────

func (s *Store) CleanupCall(ctx context.Context, streamSid string) {
	s.DeleteTakeover(ctx, streamSid)
	s.DeleteWhispers(ctx, streamSid)
}

// ─── Pub/Sub (SSE fan-out) ────────────────────────────────────────────────────

// Publish sends a message to a Redis channel.
// Silently no-ops when Redis is unavailable.
func (s *Store) Publish(ctx context.Context, channel, message string) {
	if s.rdb == nil {
		return
	}
	s.rdb.Publish(ctx, keyPrefix+channel, message)
}

// EmitCampaignEvent fan-outs a user-friendly dial event to the campaign's
// SSE subscribers. Mirrors Python's live_logs.emit_campaign_event exactly —
// same icon map, same timestamped format:
//
//	{icon} [HH:MM:SS] {lead_name} ({phone}) — {EVENT_TYPE} | {detail}
//
// Published to two channels so the frontend can subscribe to either the
// specific campaign or an "all campaigns" firehose:
//   - campaign:<campaignID>   (per-campaign)
//   - campaign:all            (firehose)
//
// Without this publisher, the SSE endpoint /api/campaign-events connects
// fine but stays silent forever — the "Live Campaign Activity" panel on the
// detail page stays empty even when calls are happening.
func (s *Store) EmitCampaignEvent(ctx context.Context, campaignID int64, leadName, phone, eventType, detail string) {
	if s.rdb == nil {
		return
	}
	icons := map[string]string{
		"dialing": "📞", "connected": "✅", "no-answer": "❌", "busy": "📵",
		"failed": "⚠️", "completed": "🎯", "dnd": "🚫", "hangup": "👋",
		"error": "💥", "started": "🚀", "finished": "🏁",
		"retry_dialing": "🔁",
	}
	icon, ok := icons[eventType]
	if !ok {
		icon = "📋"
	}
	now := time.Now().UTC()
	label := fmt.Sprintf("%s [%s] %s (%s) — %s", icon, now.In(displayTZ).Format("15:04:05"), leadName, phone, strings.ToUpper(eventType))
	if detail != "" {
		label += " | " + detail
	}
	// Frontend filters need structured fields; `label` keeps the existing
	// pre-formatted display so the UI render path stays unchanged.
	payload, err := json.Marshal(struct {
		Ts         string `json:"ts"`
		CampaignID int64  `json:"campaign_id"`
		LeadName   string `json:"lead_name"`
		Phone      string `json:"phone"`
		Status     string `json:"status"`
		Detail     string `json:"detail"`
		Label      string `json:"label"`
	}{
		Ts:         now.Format(time.RFC3339),
		CampaignID: campaignID,
		LeadName:   leadName,
		Phone:      phone,
		Status:     strings.ToUpper(eventType),
		Detail:     detail,
		Label:      label,
	})
	if err != nil {
		return
	}
	msg := string(payload)
	// Fan out to both channels — frontend can subscribe to either.
	chanSpecific := fmt.Sprintf("campaign:%d", campaignID)
	s.rdb.Publish(ctx, keyPrefix+chanSpecific, msg)
	s.rdb.Publish(ctx, keyPrefix+"campaign:all", msg)

	// Persist into capped history lists so newly-connecting SSE clients can
	// replay recent events. Two keys:
	//   - campaign:history:<id>   per-campaign, 50 newest (panel scroll)
	//   - campaign:history:all    global firehose, 200 newest (System Logs)
	// Mirrors Python's in-memory deque (maxlen=500 replayed last 20 on
	// connect). Without the "all" key the /logs "Activity" tab stayed empty
	// on first load — it subscribes to campaign:all but had no history to
	// replay, so the operator saw "Waiting for campaign activity…" even
	// when calls had just happened.
	histKey := fmt.Sprintf("%scampaign:history:%d", keyPrefix, campaignID)
	allKey := keyPrefix + "campaign:history:all"
	pipe := s.rdb.TxPipeline()
	pipe.LPush(ctx, histKey, msg)
	pipe.LTrim(ctx, histKey, 0, 49) // keep newest 50 per campaign
	pipe.Expire(ctx, histKey, 7*24*time.Hour)
	pipe.LPush(ctx, allKey, msg)
	pipe.LTrim(ctx, allKey, 0, 199) // keep newest 200 across all campaigns
	pipe.Expire(ctx, allKey, 7*24*time.Hour)
	_, _ = pipe.Exec(ctx)
}

// RecentAllCampaignEvents returns up to `limit` most-recent events across
// every campaign, chronologically. Backs the /logs "Activity" firehose
// (campaign_id=0 / "all") so the panel is not blank on initial load.
func (s *Store) RecentAllCampaignEvents(ctx context.Context, limit int) []string {
	if s.rdb == nil {
		return nil
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	items, err := s.rdb.LRange(ctx, keyPrefix+"campaign:history:all", 0, int64(limit-1)).Result()
	if err != nil || len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, v := range items {
		out[len(items)-1-i] = v
	}
	return out
}

// ClearCampaignEvents wipes the cached campaign-events history. Used by the
// /logs "Clear logs" button to reset both the per-campaign panel scroll and
// the global System Logs firehose so a page refresh shows an empty feed
// instead of replaying the trimmed-50/200 history that was just visually
// cleared on the client. Pass campaignID=0 to clear only the global
// firehose; a positive value also clears that campaign's per-campaign list.
// Errors are swallowed so a Redis hiccup doesn't fail the HTTP handler —
// the worst case is the next SSE reconnect re-replays the old events, which
// the operator can clear again.
func (s *Store) ClearCampaignEvents(ctx context.Context, campaignID int64) {
	if s.rdb == nil {
		return
	}
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, keyPrefix+"campaign:history:all")
	if campaignID > 0 {
		pipe.Del(ctx, fmt.Sprintf("%scampaign:history:%d", keyPrefix, campaignID))
	}
	_, _ = pipe.Exec(ctx)
}

// SetLogsClearedAt records a per-(user, scope) "I cleared this view at
// this time" marker. scope is "all" for the /logs firehose or a numeric
// campaign ID like "7" for a campaign detail panel. SSE replay-on-connect
// reads the marker for the exact scope it's serving and drops historical
// events with an older ts. Scoping per-scope means clicking Clear on /logs
// doesn't also blank the per-campaign Live Campaign Activity panel —
// previously a single per-user key was applied to both replays.
func (s *Store) SetLogsClearedAt(ctx context.Context, email, scope string, when time.Time) {
	if s.rdb == nil || email == "" {
		return
	}
	key := fmt.Sprintf("%slogs:clearedAt:%s:%s", keyPrefix, email, scope)
	_ = s.rdb.Set(ctx, key, when.UTC().Unix(), 7*24*time.Hour).Err()
}

// GetLogsClearedAt returns the per-(user, scope) clear marker as a unix
// timestamp, or 0 if no marker exists / Redis is down.
func (s *Store) GetLogsClearedAt(ctx context.Context, email, scope string) int64 {
	if s.rdb == nil || email == "" {
		return 0
	}
	key := fmt.Sprintf("%slogs:clearedAt:%s:%s", keyPrefix, email, scope)
	v, err := s.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0
	}
	return v
}

// RecentCampaignEvents returns up to `limit` most-recent campaign events
// (newest first in the list, but returned in chronological order so the UI
// renders top-to-bottom matching arrival order).
func (s *Store) RecentCampaignEvents(ctx context.Context, campaignID int64, limit int) []string {
	if s.rdb == nil {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	histKey := fmt.Sprintf("%scampaign:history:%d", keyPrefix, campaignID)
	// LRANGE 0..limit-1 returns newest→oldest (we LPUSH'd); reverse to
	// show oldest→newest so the live panel reads naturally.
	items, err := s.rdb.LRange(ctx, histKey, 0, int64(limit-1)).Result()
	if err != nil || len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, v := range items {
		out[len(items)-1-i] = v
	}
	return out
}

// Subscribe returns a channel that receives messages published to the given channel.
// The returned channel is closed when ctx is cancelled or Redis is unavailable.
func (s *Store) Subscribe(ctx context.Context, channel string) <-chan string {
	ch := make(chan string, 32)
	if s.rdb == nil {
		close(ch)
		return ch
	}
	sub := s.rdb.Subscribe(ctx, keyPrefix+channel)
	go func() {
		defer close(ch)
		msgCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case ch <- msg.Payload:
				default:
				}
			}
		}
	}()
	return ch
}

// ─── Live Logs ─────────────────────────────────────────────────────────────────

// GetLiveLogs returns the last n entries from the callified:live-logs Redis list.
// Returns an empty slice when Redis is unavailable.
func (s *Store) GetLiveLogs(ctx context.Context, n int) ([]string, error) {
	if s.rdb == nil {
		return []string{}, nil
	}
	if n <= 0 {
		n = 100
	}
	return s.rdb.LRange(ctx, key("live-logs"), int64(-n), -1).Result()
}

// ─── Raw Key Access ──────────────────────────────────────────────────────────
// Direct get/set for arbitrary keys (e.g. per-lead voice cache). Mirrors
// redis_store.py get_raw / set_raw. Keys are NOT prefixed — callers pass the
// full key. In-memory fallback is intentionally absent: the voice cache is
// best-effort persistence, and a missing Redis just means no cache, not an
// error path.

// GetRaw returns (value, true) on a hit, ("", false) on miss or no Redis.
func (s *Store) GetRaw(ctx context.Context, k string) (string, bool) {
	if s.rdb == nil {
		return "", false
	}
	v, err := s.rdb.Get(ctx, k).Result()
	if err != nil {
		return "", false
	}
	return v, true
}

// SetRaw stores value at key with optional TTL. ttl == 0 means no expiry.
// Errors are swallowed (and logged) — callers treat the cache as best-effort.
func (s *Store) SetRaw(ctx context.Context, k, v string, ttl time.Duration) {
	if s.rdb == nil {
		return
	}
	var err error
	if ttl > 0 {
		err = s.rdb.SetEx(ctx, k, v, ttl).Err()
	} else {
		err = s.rdb.Set(ctx, k, v, 0).Err()
	}
	if err != nil && s.log != nil {
		s.log.Warn("redis: SetRaw failed", zap.String("key", k), zap.Error(err))
	}
}

// DeleteRaw removes a single Redis key. Best-effort: errors are logged, not
// returned, because the caller (cache invalidation) can tolerate a miss.
func (s *Store) DeleteRaw(ctx context.Context, k string) {
	if s.rdb == nil {
		return
	}
	if err := s.rdb.Del(ctx, k).Err(); err != nil && s.log != nil {
		s.log.Warn("redis: DeleteRaw failed", zap.String("key", k), zap.Error(err))
	}
}

// LeadVoiceTTL is how long the per-lead voice override is remembered.
// Mirrors ws_handler.py 4aa3fa3: 90 days, so a lead reliably hears the same
// agent voice across follow-up calls.
const LeadVoiceTTL = 90 * 24 * time.Hour

// MarkBridgeAnswered records that the customer answered the bridge call.
// Called from the Exotel status webhook when Status=in-progress.
func (s *Store) MarkBridgeAnswered(ctx context.Context, callSid string) {
	if s.rdb != nil {
		s.rdb.Set(ctx, "bridge:answered:"+callSid, "1", 5*time.Minute)
	}
}

// WaitBridgeAnswered polls until MarkBridgeAnswered fires for callSid, or
// until timeout. Returns true if answered, false if timed out or cancelled.
func (s *Store) WaitBridgeAnswered(ctx context.Context, callSid string, timeout time.Duration) bool {
	if s.rdb == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if val, _ := s.rdb.Get(ctx, "bridge:answered:"+callSid).Result(); val == "1" {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

// ResolveLeadVoice returns the voice ID to use for a call to leadID. If a
// previously-used voice is cached, it wins (consistency over campaign default).
// Otherwise currentVoice is cached for next time.
// leadID == 0 disables the cache. Returns (voiceID, fromCache).
func (s *Store) ResolveLeadVoice(ctx context.Context, leadID int64, currentVoice string) (string, bool) {
	if leadID == 0 || currentVoice == "" {
		return currentVoice, false
	}
	k := fmt.Sprintf("lead_voice:%d", leadID)
	if cached, ok := s.GetRaw(ctx, k); ok && cached != "" {
		// Refresh TTL on hit so active leads don't expire.
		s.SetRaw(ctx, k, cached, LeadVoiceTTL)
		return cached, true
	}
	s.SetRaw(ctx, k, currentVoice, LeadVoiceTTL)
	return currentVoice, false
}
