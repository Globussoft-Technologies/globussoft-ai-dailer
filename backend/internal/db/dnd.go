package db

// DNDNumber mirrors the dnd_numbers table.
type DNDNumber struct {
	ID      int64  `json:"id"`
	OrgID   int64  `json:"org_id"`
	Phone   string `json:"phone"`
	Source  string `json:"source"`
	AddedAt string `json:"added_at"`
}

// GetDNDNumbers returns all DND numbers for an org ordered by id DESC.
func (d *DB) GetDNDNumbers(orgID int64) ([]DNDNumber, error) {
	rows, err := d.pool.Query(`
		SELECT id, COALESCE(org_id,0), phone, COALESCE(source,'manual'),
		DATE_FORMAT(added_at,'%Y-%m-%d %H:%i:%s')
		FROM dnd_numbers WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DNDNumber
	for rows.Next() {
		var n DNDNumber
		if err := rows.Scan(&n.ID, &n.OrgID, &n.Phone, &n.Source, &n.AddedAt); err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

// AddDNDNumber inserts a single phone into dnd_numbers. Silently ignores duplicates.
func (d *DB) AddDNDNumber(orgID int64, phone, source string) error {
	if source == "" {
		source = "manual"
	}
	_, err := d.pool.Exec(
		`INSERT IGNORE INTO dnd_numbers (org_id, phone, source) VALUES (?,?,?)`,
		orgID, phone, source)
	return err
}

// AddDNDNumbersBulk inserts many phones in a single transaction.
func (d *DB) AddDNDNumbersBulk(orgID int64, phones []string, source string) error {
	if len(phones) == 0 {
		return nil
	}
	if source == "" {
		source = "manual"
	}
	tx, err := d.pool.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT IGNORE INTO dnd_numbers (org_id, phone, source) VALUES (?,?,?)`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	defer stmt.Close()
	for _, ph := range phones {
		if _, err := stmt.Exec(orgID, ph, source); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	return tx.Commit()
}

// RemoveDNDNumber deletes a DND entry by ID (scoped to org). Returns true if deleted.
func (d *DB) RemoveDNDNumber(orgID, id int64) (bool, error) {
	res, err := d.pool.Exec(`DELETE FROM dnd_numbers WHERE id=? AND org_id=?`, id, orgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// IsDNDNumber returns true if the phone is on the DND list for the org.
func (d *DB) IsDNDNumber(orgID int64, phone string) (bool, error) {
	var count int
	err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM dnd_numbers WHERE org_id=? AND phone=?`, orgID, phone,
	).Scan(&count)
	return count > 0, err
}

// GetDNDCount returns the total number of DND entries for an org.
func (d *DB) GetDNDCount(orgID int64) (int64, error) {
	var n int64
	err := d.pool.QueryRow(`SELECT COUNT(*) FROM dnd_numbers WHERE org_id=?`, orgID).Scan(&n)
	return n, err
}
