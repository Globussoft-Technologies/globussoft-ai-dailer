package db

import (
	"database/sql"
	"errors"
)

// Site mirrors the sites table.
type Site struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	RadiusM   int    `json:"radius_m"`
	CreatedAt string `json:"created_at"`
}

// Punch mirrors the punches table.
type Punch struct {
	ID        int64   `json:"id"`
	SiteID    int64   `json:"site_id"`
	UserID    int64   `json:"user_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Status    string  `json:"status"`
	Notes     string  `json:"notes"`
	CreatedAt string  `json:"created_at"`
}

// GetSitesByOrg returns all sites for an org ordered by id DESC.
func (d *DB) GetSitesByOrg(orgID int64) ([]Site, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, name, COALESCE(address,''),
		COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(radius_m,100),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM sites WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Address,
			&s.Latitude, &s.Longitude, &s.RadiusM, &s.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetSiteByID fetches a single site. Returns nil when not found.
func (d *DB) GetSiteByID(siteID int64) (*Site, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, name, COALESCE(address,''),
		COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(radius_m,100),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM sites WHERE id=?`, siteID)
	s := &Site{}
	err := row.Scan(&s.ID, &s.OrgID, &s.Name, &s.Address,
		&s.Latitude, &s.Longitude, &s.RadiusM, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// CreatePunch inserts a punch record. Returns the new punch ID.
func (d *DB) CreatePunch(siteID, userID int64, lat, lon float64, status, notes string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO punches (site_id, user_id, latitude, longitude, status, notes)
		VALUES (?,?,?,?,?,?)`,
		siteID, userID, lat, lon, status, nullString(notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
