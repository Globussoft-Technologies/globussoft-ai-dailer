package db

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// BillingPlan mirrors the billing_plans table.
type BillingPlan struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	PricePaise       int64           `json:"price_paise"`
	MinutesIncluded  int             `json:"minutes_included"`
	ExtraMinutePaise int             `json:"extra_minute_paise"`
	BillingInterval  string          `json:"billing_interval"`
	TrialDays        int             `json:"trial_days"`
	Features         json.RawMessage `json:"features"`
	IsActive         bool            `json:"is_active"`
}

// Subscription mirrors the subscriptions table.
type Subscription struct {
	ID          int64  `json:"id"`
	OrgID       int64  `json:"org_id"`
	PlanID      int64  `json:"plan_id"`
	PlanName    string `json:"plan_name"`
	Status      string `json:"status"` // trialing, active, past_due, cancelled, expired
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	CreatedAt   string `json:"created_at"`
}

// BillingPayment mirrors the billing_payments table.
type BillingPayment struct {
	ID                int64  `json:"id"`
	OrgID             int64  `json:"org_id"`
	AmountPaise       int64  `json:"amount_paise"`
	Currency          string `json:"currency"`
	Status            string `json:"status"`
	RazorpayOrderID   string `json:"razorpay_order_id"`
	RazorpayPaymentID string `json:"razorpay_payment_id"`
	CreatedAt         string `json:"created_at"`
}

// Invoice mirrors the invoices table.
type Invoice struct {
	ID            int64  `json:"id"`
	OrgID         int64  `json:"org_id"`
	InvoiceNumber string `json:"invoice_number"`
	AmountPaise   int64  `json:"amount_paise"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

// BillingUsage holds current-period usage stats for an org.
type BillingUsage struct {
	HasSubscription bool   `json:"has_subscription"`
	PlanName        string `json:"plan_name"`
	MinutesUsed     int    `json:"minutes_used"`
	MinutesIncluded int    `json:"minutes_included"`
	MinutesRemaining int   `json:"minutes_remaining"`
	OverageMinutes  int    `json:"overage_minutes"`
	OverageCostPaise int64 `json:"overage_cost_paise"`
	PeriodEnd       string `json:"period_end"`
}

// GetBillingPlans returns all active billing plans.
func (d *DB) GetBillingPlans() ([]BillingPlan, error) {
	rows, err := d.pool.Query(`
		SELECT id, COALESCE(name,''), price_paise,
		       COALESCE(minutes_included,0), COALESCE(extra_minute_paise,0),
		       COALESCE(billing_interval,'monthly'), COALESCE(trial_days,0),
		       COALESCE(features,'[]'), COALESCE(is_active,1)
		FROM billing_plans WHERE COALESCE(is_active,1)=1 ORDER BY price_paise`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []BillingPlan
	for rows.Next() {
		var p BillingPlan
		var active int
		var featBytes []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.PricePaise,
			&p.MinutesIncluded, &p.ExtraMinutePaise,
			&p.BillingInterval, &p.TrialDays, &featBytes, &active); err != nil {
			return nil, err
		}
		p.IsActive = active == 1
		if len(featBytes) > 0 {
			p.Features = json.RawMessage(featBytes)
		} else {
			p.Features = json.RawMessage("[]")
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetSubscriptionByOrg returns the latest active/trialing subscription for an org, or nil.
func (d *DB) GetSubscriptionByOrg(orgID int64) (*Subscription, error) {
	row := d.pool.QueryRow(`
		SELECT s.id, s.org_id, s.plan_id, COALESCE(p.name,''),
		       COALESCE(s.status,''),
		       COALESCE(DATE_FORMAT(s.current_period_start,'%Y-%m-%d'),''),
		       COALESCE(DATE_FORMAT(s.current_period_end,'%Y-%m-%d'),''),
		       DATE_FORMAT(s.created_at,'%Y-%m-%d %H:%i:%s')
		FROM subscriptions s
		LEFT JOIN billing_plans p ON s.plan_id=p.id
		WHERE s.org_id=? AND s.status IN ('active','trialing')
		ORDER BY s.id DESC LIMIT 1`, orgID)
	var sub Subscription
	err := row.Scan(&sub.ID, &sub.OrgID, &sub.PlanID, &sub.PlanName,
		&sub.Status, &sub.PeriodStart, &sub.PeriodEnd, &sub.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &sub, err
}

// CreateSubscription inserts a new subscription row. Returns new ID.
func (d *DB) CreateSubscription(orgID, planID int64, billingCycle string) (int64, error) {
	// billing_cycle determines period length; monthly = 30 days, annual = 365 days
	interval := "INTERVAL 30 DAY"
	if billingCycle == "annual" || billingCycle == "yearly" {
		interval = "INTERVAL 365 DAY"
	}
	res, err := d.pool.Exec(`
		INSERT INTO subscriptions (org_id, plan_id, status, current_period_start, current_period_end)
		VALUES (?, ?, 'active', NOW(), NOW() + `+interval+`)`, orgID, planID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CancelSubscription marks the active subscription as cancelled.
func (d *DB) CancelSubscription(orgID int64) error {
	_, err := d.pool.Exec(
		`UPDATE subscriptions SET status='cancelled', cancelled_at=NOW()
		 WHERE org_id=? AND status IN ('active','trialing')`, orgID)
	return err
}

// GetBillingUsage returns current-period call usage for an org.
func (d *DB) GetBillingUsage(orgID int64) (*BillingUsage, error) {
	u := &BillingUsage{}

	// Get active subscription + plan
	var planName string
	var minutesIncluded, extraMinutePaise int
	var periodStart, periodEnd string
	err := d.pool.QueryRow(`
		SELECT COALESCE(p.name,''), COALESCE(p.minutes_included,0),
		       COALESCE(p.extra_minute_paise,0),
		       COALESCE(DATE_FORMAT(s.current_period_start,'%Y-%m-%d'),''),
		       COALESCE(DATE_FORMAT(s.current_period_end,'%Y-%m-%d'),'')
		FROM subscriptions s
		LEFT JOIN billing_plans p ON s.plan_id=p.id
		WHERE s.org_id=? AND s.status IN ('active','trialing')
		ORDER BY s.id DESC LIMIT 1`, orgID).
		Scan(&planName, &minutesIncluded, &extraMinutePaise, &periodStart, &periodEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return u, nil // no subscription
	}
	if err != nil {
		return u, err
	}

	u.HasSubscription = true
	u.PlanName = planName
	u.MinutesIncluded = minutesIncluded
	u.PeriodEnd = periodEnd

	// Count call seconds in the current period, convert to minutes
	var totalSec int
	err = d.pool.QueryRow(`
		SELECT COALESCE(SUM(call_duration_s), 0)
		FROM call_transcripts
		WHERE org_id=? AND created_at >= ?`, orgID, periodStart).Scan(&totalSec)
	if err != nil {
		return u, err
	}
	minutesUsed := totalSec / 60
	u.MinutesUsed = minutesUsed

	if minutesUsed >= minutesIncluded {
		u.OverageMinutes = minutesUsed - minutesIncluded
		u.OverageCostPaise = int64(u.OverageMinutes) * int64(extraMinutePaise)
		u.MinutesRemaining = 0
	} else {
		u.MinutesRemaining = minutesIncluded - minutesUsed
	}

	return u, nil
}

// CreateRazorpayOrder inserts a pending payment record. Returns new ID.
func (d *DB) CreateRazorpayOrder(orgID, planID int64, orderID, currency string, amount float64) (int64, error) {
	// amount is in rupees from Razorpay; convert to paise
	amountPaise := int64(amount * 100)
	res, err := d.pool.Exec(`
		INSERT INTO billing_payments (org_id, amount_paise, currency, status, razorpay_order_id)
		VALUES (?,?,?,?,?)`, orgID, amountPaise, currency, "created", orderID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CompleteRazorpayPayment marks a payment as captured and links the payment ID.
func (d *DB) CompleteRazorpayPayment(orderID, paymentID string) error {
	_, err := d.pool.Exec(
		`UPDATE billing_payments SET razorpay_payment_id=?, status='captured'
		 WHERE razorpay_order_id=?`, paymentID, orderID)
	return err
}

// GetPaymentsByOrg returns payment history for an org.
func (d *DB) GetPaymentsByOrg(orgID int64) ([]BillingPayment, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, COALESCE(amount_paise,0), COALESCE(currency,'INR'),
		       COALESCE(status,''), COALESCE(razorpay_order_id,''),
		       COALESCE(razorpay_payment_id,''),
		       DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM billing_payments WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []BillingPayment
	for rows.Next() {
		var p BillingPayment
		if err := rows.Scan(&p.ID, &p.OrgID, &p.AmountPaise, &p.Currency,
			&p.Status, &p.RazorpayOrderID, &p.RazorpayPaymentID, &p.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// CreateInvoice inserts an invoice row. Returns new ID.
func (d *DB) CreateInvoice(orgID int64, invoiceNumber, paymentID, currency string, amount float64) (int64, error) {
	amountPaise := int64(amount * 100)
	res, err := d.pool.Exec(`
		INSERT INTO invoices (org_id, invoice_number, amount_paise, status)
		VALUES (?,?,?,'paid')`, orgID, invoiceNumber, amountPaise)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetInvoicesByOrg returns invoices for an org.
func (d *DB) GetInvoicesByOrg(orgID int64) ([]Invoice, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, COALESCE(invoice_number,''), COALESCE(amount_paise,0),
		       COALESCE(status,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM invoices WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.InvoiceNumber,
			&inv.AmountPaise, &inv.Status, &inv.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, inv)
	}
	return list, rows.Err()
}

// GetInvoiceByNumber fetches a single invoice by number and orgID.
func (d *DB) GetInvoiceByNumber(orgID int64, invoiceNumber string) (*Invoice, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, COALESCE(invoice_number,''), COALESCE(amount_paise,0),
		       COALESCE(status,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM invoices WHERE org_id=? AND invoice_number=? LIMIT 1`, orgID, invoiceNumber)
	var inv Invoice
	err := row.Scan(&inv.ID, &inv.OrgID, &inv.InvoiceNumber,
		&inv.AmountPaise, &inv.Status, &inv.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &inv, err
}

// GetPaymentByOrderID returns a payment row for a Razorpay order ID.
func (d *DB) GetPaymentByOrderID(orderID string) (*BillingPayment, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, COALESCE(amount_paise,0), COALESCE(currency,'INR'),
		       COALESCE(status,''), COALESCE(razorpay_order_id,''),
		       COALESCE(razorpay_payment_id,''),
		       DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM billing_payments WHERE razorpay_order_id=? LIMIT 1`, orderID)
	var p BillingPayment
	err := row.Scan(&p.ID, &p.OrgID, &p.AmountPaise, &p.Currency,
		&p.Status, &p.RazorpayOrderID, &p.RazorpayPaymentID, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}
