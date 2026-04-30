package db

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// WAChannelSettings stores per-org WhatsApp channel credentials (Phase 3C table: wa_channel_configs).
//
// The flat columns (api_key, app_id, phone_number, webhook_url) are kept for
// backwards-compatibility with the gupshup-only sender code that still reads
// them directly. New per-provider fields (Wati's bearer_token, AiSensei's
// base_url, Meta's access_token / app_secret / verify_token, etc.) live in
// the JSON `credentials` column so any provider can store its own shape
// without further schema changes.
type WAChannelSettings struct {
	ID          int64             `json:"id"`
	OrgID       int64             `json:"org_id"`
	Provider    string            `json:"provider"` // gupshup, wati, aisensei, interakt, meta
	PhoneNumber string            `json:"phone_number"`
	APIKey      string            `json:"api_key"`
	AppID       string            `json:"app_id"`
	WebhookURL  string            `json:"webhook_url"`
	Credentials map[string]string `json:"credentials"`
	IsActive    bool              `json:"is_active"`
	AIEnabled   bool              `json:"ai_enabled"`
	CreatedAt   string            `json:"created_at"`
}

// WAMessage is a single message in a WA conversation.
type WAMessage struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversation_id"`
	Direction      string `json:"direction"` // inbound, outbound
	MessageText    string `json:"message_text"`
	MessageType    string `json:"message_type"`
	ProviderMsgID  string `json:"provider_msg_id"`
	CreatedAt      string `json:"created_at"`
}

// GetWAChannelConfigsByOrg returns all WA channel settings for an org.
func (d *DB) GetWAChannelConfigsByOrg(orgID int64) ([]WAChannelSettings, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, COALESCE(provider,''), COALESCE(phone_number,''),
		COALESCE(api_key,''), COALESCE(app_id,''), COALESCE(webhook_url,''),
		COALESCE(credentials,'{}'),
		COALESCE(is_active,1), COALESCE(ai_enabled,0),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM wa_channel_configs WHERE org_id=? ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []WAChannelSettings
	for rows.Next() {
		var c WAChannelSettings
		var active, aiEnabled int
		var credsJSON string
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Provider, &c.PhoneNumber,
			&c.APIKey, &c.AppID, &c.WebhookURL, &credsJSON,
			&active, &aiEnabled, &c.CreatedAt); err != nil {
			return nil, err
		}
		// Tolerate malformed JSON — fall back to an empty map so a single bad
		// row doesn't 500 the whole listing. The flat columns still surface
		// via APIKey/AppID/PhoneNumber so the modal isn't completely empty.
		c.Credentials = map[string]string{}
		if credsJSON != "" {
			_ = json.Unmarshal([]byte(credsJSON), &c.Credentials)
		}
		c.IsActive = active == 1
		c.AIEnabled = aiEnabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetWAChannelConfigByPhone finds an active channel config by provider + phone.
func (d *DB) GetWAChannelConfigByPhone(provider, phone string) (*WAChannelSettings, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, COALESCE(provider,''), COALESCE(phone_number,''),
		COALESCE(api_key,''), COALESCE(app_id,''), COALESCE(webhook_url,''),
		COALESCE(is_active,1), COALESCE(ai_enabled,0),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM wa_channel_configs WHERE provider=? AND phone_number=? AND is_active=1
		LIMIT 1`, provider, phone)
	var c WAChannelSettings
	var active, aiEnabled int
	err := row.Scan(&c.ID, &c.OrgID, &c.Provider, &c.PhoneNumber,
		&c.APIKey, &c.AppID, &c.WebhookURL, &active, &aiEnabled, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.IsActive = active == 1
	c.AIEnabled = aiEnabled == 1
	return &c, nil
}

// CreateWAChannelConfig inserts a new channel config. Returns new ID.
func (d *DB) CreateWAChannelConfig(orgID int64, provider, phone, apiKey, appID, webhookURL string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO wa_channel_configs (org_id, provider, phone_number, api_key, app_id, webhook_url, is_active, ai_enabled)
		VALUES (?,?,?,?,?,?,1,1)`, orgID, provider, phone, apiKey, appID, webhookURL)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateWAChannelConfig updates credentials and settings for an existing config.
func (d *DB) UpdateWAChannelConfig(id, orgID int64, apiKey, appID, webhookURL string, aiEnabled bool) error {
	ai := 0
	if aiEnabled {
		ai = 1
	}
	_, err := d.pool.Exec(`
		UPDATE wa_channel_configs SET api_key=?, app_id=?, webhook_url=?, ai_enabled=?
		WHERE id=? AND org_id=?`, apiKey, appID, webhookURL, ai, id, orgID)
	return err
}

// DeleteWAChannelConfig deactivates a channel config.
func (d *DB) DeleteWAChannelConfig(id, orgID int64) error {
	_, err := d.pool.Exec(
		`UPDATE wa_channel_configs SET is_active=0 WHERE id=? AND org_id=?`, id, orgID)
	return err
}

// UpsertWAChannelConfig inserts-or-updates the channel config for an
// (org, provider) pair. Frontend modal saves one config at a time and doesn't
// track row IDs, so we upsert on the (org_id, provider) unique key rather
// than branching on insert-vs-update in application code. autoReply nil
// means "don't change the stored value" (modal toggle is optional).
//
// `creds` is the full credentials map posted by the frontend; it's serialised
// into the `credentials` JSON column so per-provider fields that don't have
// a flat column (Wati's bearer_token, AiSensei's base_url, Meta's
// access_token / app_secret / verify_token, etc.) round-trip cleanly. The
// flat columns (api_key, app_id, phone_number) stay populated for the
// gupshup-only sender code that still reads them directly — the API handler
// extracts those three from `creds` and passes them in.
func (d *DB) UpsertWAChannelConfig(orgID int64, provider, phone, apiKey, appID, webhookURL string, creds map[string]string, autoReply *bool) (int64, error) {
	ai := 1
	if autoReply != nil && !*autoReply {
		ai = 0
	}
	if creds == nil {
		creds = map[string]string{}
	}
	credsJSON, err := json.Marshal(creds)
	if err != nil {
		return 0, err
	}
	res, err := d.pool.Exec(`
		INSERT INTO wa_channel_configs
			(org_id, provider, phone_number, api_key, app_id, webhook_url, credentials,
			 is_active, ai_enabled, auto_reply)
		VALUES (?,?,?,?,?,?,?,1,?,?)
		ON DUPLICATE KEY UPDATE
			phone_number=VALUES(phone_number),
			api_key=VALUES(api_key),
			app_id=VALUES(app_id),
			webhook_url=VALUES(webhook_url),
			credentials=VALUES(credentials),
			is_active=1,
			ai_enabled=VALUES(ai_enabled),
			auto_reply=VALUES(auto_reply)`,
		orgID, provider, phone, apiKey, appID, webhookURL, string(credsJSON), ai, ai)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// GetWAConversationIDByPhone resolves a contact phone to the internal
// conversation PK for this org. Returns 0 when no conversation exists yet
// (frontend renders an empty message list in that case, not a 500).
func (d *DB) GetWAConversationIDByPhone(orgID int64, phone string) (int64, error) {
	var id int64
	err := d.pool.QueryRow(
		`SELECT id FROM whatsapp_conversations
		 WHERE org_id=? AND phone=?
		 ORDER BY updated_at DESC LIMIT 1`, orgID, phone).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ToggleWAConversationAI flips ai_enabled on a single conversation
// (phone-scoped) so the operator can mute AI for one contact without
// affecting other threads on the same channel.
func (d *DB) ToggleWAConversationAI(orgID int64, phone string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := d.pool.Exec(
		`UPDATE whatsapp_conversations SET ai_enabled=?
		 WHERE org_id=? AND phone=?`, v, orgID, phone)
	return err
}

// ToggleWAAI enables/disables AI for a channel config.
func (d *DB) ToggleWAAI(id, orgID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := d.pool.Exec(
		`UPDATE wa_channel_configs SET ai_enabled=? WHERE id=? AND org_id=?`, v, id, orgID)
	return err
}

// GetWAConversationsList returns recent conversations for an org.
func (d *DB) GetWAConversationsList(orgID int64, limit int) ([]WAConversationRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.pool.Query(`
		SELECT c.id, c.org_id, c.phone, COALESCE(c.provider,''),
		COALESCE(c.last_message,''), COALESCE(c.message_count,0),
		COALESCE(c.lead_id,0), COALESCE(l.first_name,''),
		DATE_FORMAT(c.updated_at,'%Y-%m-%d %H:%i:%s')
		FROM whatsapp_conversations c
		LEFT JOIN leads l ON c.lead_id=l.id
		WHERE c.org_id=?
		ORDER BY c.updated_at DESC
		LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []WAConversationRow
	for rows.Next() {
		var row WAConversationRow
		if err := rows.Scan(&row.ID, &row.OrgID, &row.Phone, &row.Provider,
			&row.LastMessage, &row.MessageCount, &row.LeadID, &row.LeadName,
			&row.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

// GetWAChatHistory returns messages for a conversation.
func (d *DB) GetWAChatHistory(conversationID int64, limit int) ([]WAMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.pool.Query(`
		SELECT id, conversation_id,
		COALESCE(direction,'inbound'), COALESCE(message_text,''),
		COALESCE(message_type,'text'), COALESCE(provider_msg_id,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM whatsapp_messages
		WHERE conversation_id=?
		ORDER BY id DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []WAMessage
	for rows.Next() {
		var m WAMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Direction,
			&m.MessageText, &m.MessageType, &m.ProviderMsgID, &m.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// GetWAMessageByProviderID finds a message by its provider-assigned ID (dedup).
func (d *DB) GetWAMessageByProviderID(providerMsgID string) (*WAMessage, error) {
	row := d.pool.QueryRow(`
		SELECT id, conversation_id, COALESCE(direction,''), COALESCE(message_text,''),
		COALESCE(message_type,'text'), COALESCE(provider_msg_id,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM whatsapp_messages WHERE provider_msg_id=? LIMIT 1`, providerMsgID)
	var m WAMessage
	err := row.Scan(&m.ID, &m.ConversationID, &m.Direction, &m.MessageText,
		&m.MessageType, &m.ProviderMsgID, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// GetOrCreateWAConversation upserts a conversation row and returns the ID.
func (d *DB) GetOrCreateWAConversation(orgID int64, phone, provider string) (int64, error) {
	_, err := d.pool.Exec(`
		INSERT INTO whatsapp_conversations (org_id, phone, provider, message_count)
		VALUES (?,?,?,0)
		ON DUPLICATE KEY UPDATE updated_at=NOW()`, orgID, phone, provider)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.pool.QueryRow(
		`SELECT id FROM whatsapp_conversations WHERE org_id=? AND phone=? AND provider=? LIMIT 1`,
		orgID, phone, provider).Scan(&id)
	return id, err
}

// SaveWAMessage inserts a message and updates the conversation.
func (d *DB) SaveWAMessage(conversationID int64, direction, text, msgType, providerMsgID string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO whatsapp_messages (conversation_id, direction, message_text, message_type, provider_msg_id)
		VALUES (?,?,?,?,?)`, conversationID, direction, text, msgType, providerMsgID)
	if err != nil {
		return 0, err
	}
	d.pool.Exec( //nolint:errcheck
		`UPDATE whatsapp_conversations SET last_message=?, message_count=message_count+1, updated_at=NOW()
		WHERE id=?`, text, conversationID)
	return res.LastInsertId()
}
