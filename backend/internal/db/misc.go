package db

// DemoRequest mirrors the demo_requests table.
type DemoRequest struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Company   string `json:"company"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// CreateDemoRequest inserts a new demo request and returns its ID.
func (d *DB) CreateDemoRequest(name, email, phone, company, message string) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO demo_requests (name, email, phone, company, message) VALUES (?,?,?,?,?)`,
		name, email, nullString(phone), nullString(company), nullString(message))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetAllDemoRequests returns all demo requests ordered by id DESC.
func (d *DB) GetAllDemoRequests() ([]DemoRequest, error) {
	rows, err := d.pool.Query(`
		SELECT id, name, email, COALESCE(phone,''), COALESCE(company,''), COALESCE(message,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM demo_requests ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DemoRequest
	for rows.Next() {
		var dr DemoRequest
		if err := rows.Scan(&dr.ID, &dr.Name, &dr.Email, &dr.Phone,
			&dr.Company, &dr.Message, &dr.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, dr)
	}
	return list, rows.Err()
}
