package db

import (
	"database/sql"
	"errors"
)

// WAChannelSettings stores per-org WhatsApp channel credentials (Phase 3C table: wa_channel_configs).
type WAChannelSettings struct {
	ID          int64  `json:"id"`
	OrgID       int64  `json:"org_id"`
	Provider    string `json:"provider"` // gupshup, wati, aisensei, interakt, meta
	PhoneNumber string `json:"phone_number"`
	APIKey      string `json:"api_key"`
	AppID       string `json:"app_id"`
	WebhookURL  string `json:"webhook_url"`
	IsActive    bool   `json:"is_active"`
	AIEnabled   bool   `json:"ai_enabled"`
	CreatedAt   string `json:"created_at"`
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
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Provider, &c.PhoneNumber,
			&c.APIKey, &c.AppID, &c.WebhookURL, &active, &aiEnabled, &c.CreatedAt); err != nil {
			return nil, err
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
