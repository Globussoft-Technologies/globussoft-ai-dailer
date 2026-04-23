package db

import (
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// User mirrors the users table row.
type User struct {
	ID           int64  `json:"id"`
	OrgID        int64  `json:"org_id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	FullName     string `json:"full_name"`
	Role         string `json:"role"`
}

// GetUserByEmail fetches a user by email. Returns nil, nil when not found.
func (d *DB) GetUserByEmail(email string) (*User, error) {
	row := d.pool.QueryRow(
		`SELECT id, COALESCE(org_id,0), email, password_hash, COALESCE(full_name,''), COALESCE(role,'Admin')
		 FROM users WHERE email = ?`, email)
	u := &User{}
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

// CreateUser inserts a new user and returns its ID.
func (d *DB) CreateUser(email, passwordHash, fullName, role string, orgID int64) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO users (email, password_hash, full_name, role, org_id) VALUES (?,?,?,?,?)`,
		email, passwordHash, fullName, role, nullInt64(orgID),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetTeamMembers returns all users belonging to the given org.
func (d *DB) GetTeamMembers(orgID int64) ([]User, error) {
	rows, err := d.pool.Query(
		`SELECT id, COALESCE(org_id,0), email, '', COALESCE(full_name,''), COALESCE(role,'Member')
		 FROM users WHERE org_id=? ORDER BY id ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName, &u.Role); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// CreateUserWithRole creates a user already assigned to an org (team invite flow).
func (d *DB) CreateUserWithRole(email, passwordHash, fullName, role string, orgID int64) (int64, error) {
	return d.CreateUser(email, passwordHash, fullName, role, orgID)
}

// UpdateUserRole changes the role column for a user.
func (d *DB) UpdateUserRole(userID int64, role string) error {
	_, err := d.pool.Exec(`UPDATE users SET role=? WHERE id=?`, role, userID)
	return err
}

// DeleteUser removes a user scoped to an org (prevents cross-org deletion).
func (d *DB) DeleteUser(userID, orgID int64) error {
	_, err := d.pool.Exec(`DELETE FROM users WHERE id=? AND org_id=?`, userID, orgID)
	return err
}

// HashPassword returns a bcrypt hash of the plain-text password.
// Compatible with Python passlib's bcrypt scheme.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword verifies a plain-text password against a bcrypt hash.
func CheckPassword(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Organization is a minimal org row (used during auth).
type OrgRow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GetOrganization fetches org name by id. Returns nil when not found.
func (d *DB) GetOrganization(orgID int64) (*OrgRow, error) {
	row := d.pool.QueryRow(`SELECT id, name FROM organizations WHERE id = ?`, orgID)
	o := &OrgRow{}
	err := row.Scan(&o.ID, &o.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return o, err
}

// CreateOrganization inserts a new org and returns its ID.
func (d *DB) CreateOrganization(name string) (int64, error) {
	res, err := d.pool.Exec(`INSERT INTO organizations (name) VALUES (?)`, name)
	if err != nil {
		return 0, fmt.Errorf("CreateOrganization: %w", err)
	}
	return res.LastInsertId()
}
