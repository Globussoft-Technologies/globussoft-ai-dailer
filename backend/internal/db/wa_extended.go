package db

// WAConversationRow is a row from whatsapp_conversations joined with lead info.
type WAConversationRow struct {
	ID           int64  `json:"id"`
	OrgID        int64  `json:"org_id"`
	Phone        string `json:"phone"`
	Provider     string `json:"provider"`
	LastMessage  string `json:"last_message"`
	MessageCount int    `json:"message_count"`
	LeadID       int64  `json:"lead_id"`
	LeadName     string `json:"lead_name"`
	UpdatedAt    string `json:"updated_at"`
}

// GetAllWhatsappLogs returns recent outbound/inbound WA conversation rows for an org.
func (d *DB) GetAllWhatsappLogs(orgID int64) ([]WAConversationRow, error) {
	rows, err := d.pool.Query(`
		SELECT c.id, c.org_id, c.phone, COALESCE(c.provider,''),
		COALESCE(c.last_message,''), COALESCE(c.message_count,0),
		COALESCE(c.lead_id,0), COALESCE(l.first_name,''),
		DATE_FORMAT(c.updated_at,'%Y-%m-%d %H:%i:%s')
		FROM whatsapp_conversations c
		LEFT JOIN leads l ON c.lead_id=l.id
		WHERE c.org_id=?
		ORDER BY c.updated_at DESC
		LIMIT 200`, orgID)
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
