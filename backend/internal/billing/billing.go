// Package billing handles Razorpay payments and subscription management.
package billing

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/email"
)

// Service orchestrates billing operations.
type Service struct {
	db       *db.DB
	razorpay *RazorpayClient
	email    *email.Service
	log      *zap.Logger
}

// New creates a billing Service.
func New(database *db.DB, keyID, keySecret string, emailSvc *email.Service, log *zap.Logger) *Service {
	return &Service{
		db:       database,
		razorpay: newRazorpayClient(keyID, keySecret),
		email:    emailSvc,
		log:      log,
	}
}

// CreateOrder creates a Razorpay order for a plan purchase.
// Returns (orderID, error).
func (s *Service) CreateOrder(ctx context.Context, orgID, planID int64, billingCycle string) (string, error) {
	plans, err := s.db.GetBillingPlans()
	if err != nil {
		return "", err
	}
	var plan *db.BillingPlan
	for i := range plans {
		if plans[i].ID == planID {
			plan = &plans[i]
			break
		}
	}
	if plan == nil {
		return "", fmt.Errorf("plan %d not found", planID)
	}

	amountPaise := plan.PricePaise
	receipt := fmt.Sprintf("org%d-plan%d-%d", orgID, planID, time.Now().Unix())
	notes := map[string]string{
		"org_id":        fmt.Sprintf("%d", orgID),
		"plan_id":       fmt.Sprintf("%d", planID),
		"billing_cycle": billingCycle,
	}

	orderID, err := s.razorpay.CreateOrder(ctx, amountPaise, "INR", receipt, notes)
	if err != nil {
		return "", fmt.Errorf("razorpay CreateOrder: %w", err)
	}

	if _, err := s.db.CreateRazorpayOrder(orgID, planID, orderID, "INR", float64(amountPaise)/100); err != nil {
		s.log.Warn("billing: CreateRazorpayOrder DB failed", zap.Error(err))
	}

	return orderID, nil
}

// VerifyAndActivate verifies a payment signature and activates the subscription.
// planID must be passed from the API layer since the payment record no longer stores it.
// Returns the invoice number.
func (s *Service) VerifyAndActivate(ctx context.Context, orgID, planID int64, orderID, paymentID, signature, billingCycle string) (string, error) {
	if !s.razorpay.VerifySignature(orderID, paymentID, signature) {
		return "", fmt.Errorf("invalid payment signature")
	}

	payment, err := s.db.GetPaymentByOrderID(orderID)
	if err != nil || payment == nil {
		return "", fmt.Errorf("payment record not found for order %s", orderID)
	}

	if err := s.db.CompleteRazorpayPayment(orderID, paymentID); err != nil {
		s.log.Warn("billing: CompleteRazorpayPayment failed", zap.Error(err))
	}

	if _, err := s.db.CreateSubscription(orgID, planID, billingCycle); err != nil {
		s.log.Warn("billing: CreateSubscription failed", zap.Error(err))
	}

	invoiceNumber := fmt.Sprintf("INV-%d-%s", time.Now().Unix(), paymentID[:8])
	if _, err := s.db.CreateInvoice(orgID, invoiceNumber, paymentID, "INR", float64(payment.AmountPaise)/100); err != nil {
		s.log.Warn("billing: CreateInvoice failed", zap.Error(err))
	}

	return invoiceNumber, nil
}

// Razorpay returns the underlying Razorpay client (for webhook verification).
func (s *Service) Razorpay() *RazorpayClient { return s.razorpay }
