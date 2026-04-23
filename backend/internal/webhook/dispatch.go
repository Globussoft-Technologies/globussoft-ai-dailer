// Package webhook provides fire-and-forget HTTP webhook dispatch with HMAC-SHA256 signing.
// Port of webhook_dispatch.py.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
)

// Dispatcher fans out webhook events to all registered org endpoints.
type Dispatcher struct {
	database *db.DB
	client   *http.Client
	log      *zap.Logger
}

// New creates a Dispatcher with a 10-second HTTP timeout per delivery.
func New(database *db.DB, log *zap.Logger) *Dispatcher {
	return &Dispatcher{
		database: database,
		client:   &http.Client{Timeout: 10 * time.Second},
		log:      log,
	}
}

// Dispatch sends event+data to all active webhooks for the org asynchronously.
// It is fire-and-forget: returns immediately; each delivery runs in its own goroutine.
func (d *Dispatcher) Dispatch(ctx context.Context, orgID int64, event string, data any) {
	hooks, err := d.database.GetActiveWebhooksForEvent(orgID, event)
	if err != nil {
		d.log.Warn("webhook dispatch: fetch hooks", zap.Error(err))
		return
	}
	for _, wh := range hooks {
		wh := wh
		go d.deliver(wh, event, data)
	}
}

func (d *Dispatcher) deliver(wh db.Webhook, event string, data any) {
	payload := map[string]any{
		"event":     event,
		"org_id":    wh.OrgID,
		"timestamp": time.Now().Unix(),
		"data":      data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		d.log.Warn("webhook deliver: marshal", zap.Error(err))
		return
	}

	req, err := http.NewRequest(http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		d.log.Warn("webhook deliver: create request", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Callified-Event", event)
	if wh.SecretKey != "" {
		req.Header.Set("X-Callified-Signature", computeHMAC(wh.SecretKey, body))
	}

	resp, err := d.client.Do(req)
	statusCode := 0
	respMsg := ""
	if err != nil {
		respMsg = err.Error()
	} else {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if logErr := d.database.LogWebhookDelivery(wh.ID, event, statusCode, respMsg); logErr != nil {
		d.log.Warn("webhook deliver: log failure", zap.Error(logErr))
	}
	d.log.Info("webhook delivered",
		zap.Int64("webhook_id", wh.ID),
		zap.String("event", event),
		zap.Int("status", statusCode),
	)
}

// computeHMAC returns hex(HMAC-SHA256(secret, payload)).
func computeHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature validates a webhook signature header value against the payload.
func VerifySignature(secret string, payload []byte, sig string) bool {
	return hmac.Equal([]byte(computeHMAC(secret, payload)), []byte(sig))
}
