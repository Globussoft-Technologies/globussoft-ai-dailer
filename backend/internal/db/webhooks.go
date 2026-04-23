package db

// Webhook mirrors the webhooks table.
type Webhook struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	URL       string `json:"url"`
	Event     string `json:"event"`
	SecretKey string `json:"secret_key,omitempty"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
}

// WebhookLog mirrors the webhook_logs table.
type WebhookLog struct {
	ID         int64  `json:"id"`
	WebhookID  int64  `json:"webhook_id"`
	Event      string `json:"event"`
	StatusCode int    `json:"status_code"`
	Response   string `json:"response"`
	CreatedAt  string `json:"created_at"`
}

// CreateWebhook inserts a new webhook and returns its ID.
func (d *DB) CreateWebhook(orgID int64, url, event, secretKey string) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO webhooks (org_id, url, event, secret_key, active) VALUES (?,?,?,?,1)`,
		orgID, url, event, nullString(secretKey))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetWebhooksByOrg returns all webhooks for an org ordered by id DESC.
func (d *DB) GetWebhooksByOrg(orgID int64) ([]Webhook, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, url, event, COALESCE(secret_key,''), COALESCE(active,1),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM webhooks WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Webhook
	for rows.Next() {
		var wh Webhook
		var active int
		if err := rows.Scan(&wh.ID, &wh.OrgID, &wh.URL, &wh.Event,
			&wh.SecretKey, &active, &wh.CreatedAt); err != nil {
			return nil, err
		}
		wh.Active = active == 1
		list = append(list, wh)
	}
	return list, rows.Err()
}

// DeleteWebhook removes a webhook (scoped to org). Returns true if deleted.
func (d *DB) DeleteWebhook(orgID, id int64) (bool, error) {
	res, err := d.pool.Exec(`DELETE FROM webhooks WHERE id=? AND org_id=?`, id, orgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetWebhookLogs returns recent delivery logs for one webhook.
func (d *DB) GetWebhookLogs(webhookID int64, limit int) ([]WebhookLog, error) {
	rows, err := d.pool.Query(`
		SELECT id, webhook_id, event, COALESCE(status_code,0), COALESCE(response,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM webhook_logs WHERE webhook_id=? ORDER BY id DESC LIMIT ?`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []WebhookLog
	for rows.Next() {
		var l WebhookLog
		if err := rows.Scan(&l.ID, &l.WebhookID, &l.Event,
			&l.StatusCode, &l.Response, &l.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}

// GetActiveWebhooksForEvent returns all active webhooks that match an event for an org.
func (d *DB) GetActiveWebhooksForEvent(orgID int64, event string) ([]Webhook, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, url, event, COALESCE(secret_key,''), COALESCE(active,1),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM webhooks WHERE org_id=? AND event=? AND active=1`, orgID, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Webhook
	for rows.Next() {
		var wh Webhook
		var active int
		if err := rows.Scan(&wh.ID, &wh.OrgID, &wh.URL, &wh.Event,
			&wh.SecretKey, &active, &wh.CreatedAt); err != nil {
			return nil, err
		}
		wh.Active = active == 1
		list = append(list, wh)
	}
	return list, rows.Err()
}

// LogWebhookDelivery inserts a delivery record into webhook_logs.
func (d *DB) LogWebhookDelivery(webhookID int64, event string, statusCode int, response string) error {
	_, err := d.pool.Exec(
		`INSERT INTO webhook_logs (webhook_id, event, status_code, response) VALUES (?,?,?,?)`,
		webhookID, event, statusCode, nullString(response))
	return err
}
