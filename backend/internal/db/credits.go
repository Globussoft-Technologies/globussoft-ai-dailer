package db

import (
	"database/sql"
	"errors"
)

// DefaultRatePerMinPaise is ₹5/min — 500 paise. Per-org rate is stored on the
// org_credits row, but newly created rows default to this.
const DefaultRatePerMinPaise = 500

// OrgCredit mirrors the org_credits table — a single ledger balance per org.
type OrgCredit struct {
	OrgID            int64  `json:"org_id"`
	BalancePaise     int64  `json:"balance_paise"`
	RatePerMinPaise  int    `json:"rate_per_min_paise"`
	MinutesAvailable int    `json:"minutes_available"` // derived: balance / rate
	UpdatedAt        string `json:"updated_at"`
}

// CreditTransaction mirrors a single ledger entry (topup, deduction, refund).
type CreditTransaction struct {
	ID                int64   `json:"id"`
	OrgID             int64   `json:"org_id"`
	DeltaPaise        int64   `json:"delta_paise"`
	BalanceAfterPaise int64   `json:"balance_after_paise"`
	Type              string  `json:"type"` // purchase | call_deduction | refund | manual_adjust
	Reference         string  `json:"reference"`
	CallDurationS     float64 `json:"call_duration_s"`
	RatePerMinPaise   int     `json:"rate_per_min_paise"`
	Notes             string  `json:"notes"`
	CreatedAt         string  `json:"created_at"`
}

// GetOrgCredit returns the org's credit balance, creating the row at the
// default rate when none exists yet (lazy init avoids a separate signup-time
// migration). Always returns a non-nil pointer for found-or-created.
func (d *DB) GetOrgCredit(orgID int64) (*OrgCredit, error) {
	row := d.pool.QueryRow(`
		SELECT org_id, balance_paise, COALESCE(rate_per_min_paise, ?),
		       DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s')
		FROM org_credits WHERE org_id=?`, DefaultRatePerMinPaise, orgID)
	var oc OrgCredit
	err := row.Scan(&oc.OrgID, &oc.BalancePaise, &oc.RatePerMinPaise, &oc.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// First call ever for this org — create at default rate.
		if _, err := d.pool.Exec(
			`INSERT INTO org_credits (org_id, balance_paise, rate_per_min_paise) VALUES (?, 0, ?)`,
			orgID, DefaultRatePerMinPaise); err != nil {
			return nil, err
		}
		return &OrgCredit{
			OrgID:            orgID,
			BalancePaise:     0,
			RatePerMinPaise:  DefaultRatePerMinPaise,
			MinutesAvailable: 0,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	if oc.RatePerMinPaise <= 0 {
		oc.RatePerMinPaise = DefaultRatePerMinPaise
	}
	oc.MinutesAvailable = int(oc.BalancePaise / int64(oc.RatePerMinPaise))
	return &oc, nil
}

// AddCredits atomically adds delta paise to the balance and writes a ledger
// row. delta must be positive for purchases / refunds (negative for deductions
// — use DeductCallCredits for that path). reference identifies the source
// (razorpay payment id, call_sid, etc.) so the ledger can be reconciled.
//
// Wrapped in a single SQL transaction so the balance update and the ledger
// insert never disagree — partial failures during a topup would otherwise
// leave money paid but no balance change (or vice versa).
func (d *DB) AddCredits(orgID int64, deltaPaise int64, txType, reference, notes string) (int64, error) {
	tx, err := d.pool.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// Make sure the row exists at the default rate.
	if _, err := tx.Exec(
		`INSERT IGNORE INTO org_credits (org_id, balance_paise, rate_per_min_paise) VALUES (?, 0, ?)`,
		orgID, DefaultRatePerMinPaise); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(
		`UPDATE org_credits SET balance_paise = balance_paise + ? WHERE org_id=?`,
		deltaPaise, orgID); err != nil {
		return 0, err
	}

	var balanceAfter int64
	var ratePerMin int
	if err := tx.QueryRow(
		`SELECT balance_paise, COALESCE(rate_per_min_paise, ?) FROM org_credits WHERE org_id=?`,
		DefaultRatePerMinPaise, orgID,
	).Scan(&balanceAfter, &ratePerMin); err != nil {
		return 0, err
	}

	res, err := tx.Exec(`
		INSERT INTO credit_transactions
		  (org_id, delta_paise, balance_after_paise, type, reference, rate_per_min_paise, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		orgID, deltaPaise, balanceAfter, txType, nullString(reference), ratePerMin, nullString(notes))
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetOrgCreditMinutes sets the org's available calling minutes to an absolute
// value and records the adjustment in the credit ledger.
func (d *DB) SetOrgCreditMinutes(orgID int64, minutes int, reference, notes string) (int64, error) {
	if minutes < 0 {
		minutes = 0
	}
	tx, err := d.pool.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`INSERT IGNORE INTO org_credits (org_id, balance_paise, rate_per_min_paise) VALUES (?, 0, ?)`,
		orgID, DefaultRatePerMinPaise); err != nil {
		return 0, err
	}

	var currentBalance int64
	var ratePerMin int
	if err := tx.QueryRow(
		`SELECT balance_paise, COALESCE(rate_per_min_paise, ?) FROM org_credits WHERE org_id=?`,
		DefaultRatePerMinPaise, orgID,
	).Scan(&currentBalance, &ratePerMin); err != nil {
		return 0, err
	}
	if ratePerMin <= 0 {
		ratePerMin = DefaultRatePerMinPaise
	}

	newBalance := int64(minutes) * int64(ratePerMin)
	delta := newBalance - currentBalance
	if _, err := tx.Exec(
		`UPDATE org_credits SET balance_paise = ? WHERE org_id=?`,
		newBalance, orgID); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(`
		INSERT INTO credit_transactions
		  (org_id, delta_paise, balance_after_paise, type, reference, rate_per_min_paise, notes)
		VALUES (?, ?, ?, 'manual_adjust', ?, ?, ?)`,
		orgID, delta, newBalance, nullString(reference), ratePerMin, nullString(notes)); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newBalance, nil
}

// DeductCallCredits charges the lesser of (duration*rate, current balance) so
// a call already in progress isn't billed for amounts the org doesn't have.
// The dialer enforces "must have balance" at dial time; this function deals
// with the leftover seconds at the very end of a long call where the balance
// might dip to zero mid-call.
//
// Returns (deducted_paise, balance_after_paise, error). Idempotent on the
// reference (call_sid) — calling twice for the same call is a no-op.
func (d *DB) DeductCallCredits(orgID int64, callSid string, durationS float64) (int64, int64, error) {
	if durationS <= 0 || callSid == "" {
		return 0, 0, nil
	}

	tx, err := d.pool.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// Idempotency: if we've already deducted for this call_sid, return the
	// existing entry's after-balance and skip the second hit. Important
	// because recording.Service.SaveAndAnalyze can race with manual webhook
	// retries when Exotel sends "completed" twice.
	var existing int64
	err = tx.QueryRow(
		`SELECT id FROM credit_transactions WHERE reference=? AND type='call_deduction' LIMIT 1`,
		callSid).Scan(&existing)
	if err == nil {
		// already deducted — short-circuit
		var balance int64
		_ = tx.QueryRow(`SELECT balance_paise FROM org_credits WHERE org_id=?`, orgID).Scan(&balance)
		_ = tx.Commit()
		return 0, balance, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}

	// Lazy-init the org_credits row.
	if _, err := tx.Exec(
		`INSERT IGNORE INTO org_credits (org_id, balance_paise, rate_per_min_paise) VALUES (?, 0, ?)`,
		orgID, DefaultRatePerMinPaise); err != nil {
		return 0, 0, err
	}

	var balance int64
	var ratePerMin int
	if err := tx.QueryRow(
		`SELECT balance_paise, COALESCE(rate_per_min_paise, ?) FROM org_credits WHERE org_id=?`,
		DefaultRatePerMinPaise, orgID,
	).Scan(&balance, &ratePerMin); err != nil {
		return 0, 0, err
	}

	// Round up to the nearest second × rate/60 — the dialer industry charges
	// per-second once a call connects. Cap at current balance so we never
	// produce a negative ledger row from an in-flight call.
	chargePaise := int64(durationS*float64(ratePerMin)/60.0 + 0.999999)
	if chargePaise <= 0 {
		_ = tx.Commit()
		return 0, balance, nil
	}
	if chargePaise > balance {
		chargePaise = balance
	}

	if _, err := tx.Exec(
		`UPDATE org_credits SET balance_paise = balance_paise - ? WHERE org_id=?`,
		chargePaise, orgID); err != nil {
		return 0, 0, err
	}

	balanceAfter := balance - chargePaise
	if _, err := tx.Exec(`
		INSERT INTO credit_transactions
		  (org_id, delta_paise, balance_after_paise, type, reference, call_duration_s, rate_per_min_paise)
		VALUES (?, ?, ?, 'call_deduction', ?, ?, ?)`,
		orgID, -chargePaise, balanceAfter, callSid, durationS, ratePerMin); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return chargePaise, balanceAfter, nil
}

// HasCallDeductions returns true if the org has ever been billed for a call.
// Used by the dial credit gate: an org with balance=0 but no deduction history
// has never topped up – block only if they've previously consumed credits and
// run out, not just because they're a new org with a fresh zero balance.
func (d *DB) HasCallDeductions(orgID int64) (bool, error) {
	var count int
	err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM credit_transactions WHERE org_id=? AND type='call_deduction' LIMIT 1`,
		orgID).Scan(&count)
	return count > 0, err
}

// GetCreditTransactions returns the most-recent N entries for an org
// (newest first). Used by the Billing page ledger view.
func (d *DB) GetCreditTransactions(orgID int64, limit int) ([]CreditTransaction, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := d.pool.Query(`
		SELECT id, org_id, delta_paise, balance_after_paise,
		       COALESCE(type,''), COALESCE(reference,''),
		       COALESCE(call_duration_s,0), COALESCE(rate_per_min_paise,0),
		       COALESCE(notes,''),
		       DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM credit_transactions WHERE org_id=?
		ORDER BY id DESC LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CreditTransaction
	for rows.Next() {
		var t CreditTransaction
		if err := rows.Scan(&t.ID, &t.OrgID, &t.DeltaPaise, &t.BalanceAfterPaise,
			&t.Type, &t.Reference, &t.CallDurationS, &t.RatePerMinPaise,
			&t.Notes, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
