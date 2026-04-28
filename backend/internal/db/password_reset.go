package db

import (
	"database/sql"
	"errors"
	"time"
)

// PasswordResetToken mirrors a row from password_reset_tokens.
type PasswordResetToken struct {
	ID        int64
	UserID    int64
	Token     string
	ExpiresAt time.Time
	Used      bool
}

// CreateResetToken inserts a fresh reset-token row. Caller generates the
// token (32 bytes of url-safe randomness, per Python auth.py forgot_password).
func (d *DB) CreateResetToken(userID int64, token string, expiresAt time.Time) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO password_reset_tokens (user_id, token, expires_at) VALUES (?,?,?)`,
		userID, token, expiresAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetValidResetToken returns the row for `token` only when it is unused AND
// not expired. Returns (nil, nil) on "no such valid token" so handlers can
// produce a clean 400 without distinguishing between expired/used/missing.
func (d *DB) GetValidResetToken(token string) (*PasswordResetToken, error) {
	row := d.pool.QueryRow(
		`SELECT id, user_id, token, expires_at, COALESCE(used,0)
		 FROM password_reset_tokens
		 WHERE token=? AND COALESCE(used,0)=0 AND expires_at > NOW()
		 LIMIT 1`, token)
	var t PasswordResetToken
	var used int
	err := row.Scan(&t.ID, &t.UserID, &t.Token, &t.ExpiresAt, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Used = used == 1
	return &t, nil
}

// MarkResetTokenUsed flips used=1 so the token can never be replayed.
func (d *DB) MarkResetTokenUsed(id int64) error {
	_, err := d.pool.Exec(`UPDATE password_reset_tokens SET used=1 WHERE id=?`, id)
	return err
}

// UpdateUserPassword replaces a user's bcrypt hash. Caller hashes via
// db.HashPassword before calling.
func (d *DB) UpdateUserPassword(userID int64, hash string) error {
	_, err := d.pool.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
	return err
}
