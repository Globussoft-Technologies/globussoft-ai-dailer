package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RazorpayClient wraps the Razorpay REST API.
type RazorpayClient struct {
	keyID     string
	keySecret string
	client    *http.Client
}

// newRazorpayClient creates a RazorpayClient.
func newRazorpayClient(keyID, keySecret string) *RazorpayClient {
	return &RazorpayClient{
		keyID:     keyID,
		keySecret: keySecret,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateOrder creates a Razorpay order and returns the order ID.
func (c *RazorpayClient) CreateOrder(ctx context.Context, amountPaise int64, currency, receipt string, notes map[string]string) (string, error) {
	payload := map[string]any{
		"amount":   amountPaise,
		"currency": currency,
		"receipt":  receipt,
		"notes":    notes,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.razorpay.com/v1/orders", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.keyID, c.keySecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("razorpay: HTTP %d — %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// VerifySignature validates a Razorpay payment signature.
// signature = HMAC-SHA256(orderID + "|" + paymentID, keySecret)
func (c *RazorpayClient) VerifySignature(orderID, paymentID, signature string) bool {
	mac := hmac.New(sha256.New, []byte(c.keySecret))
	mac.Write([]byte(orderID + "|" + paymentID))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyWebhookSignature validates a Razorpay webhook payload signature.
func VerifyWebhookSignature(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
