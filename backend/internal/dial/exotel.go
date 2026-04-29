package dial

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ExotelClient calls the Exotel Connect API.
type ExotelClient struct {
	apiKey     string
	apiToken   string
	accountSID string
	callerID   string
	appID      string
	client     *http.Client
}

// NewExotelClient creates an Exotel REST client.
func NewExotelClient(apiKey, apiToken, accountSID, callerID, appID string) *ExotelClient {
	return &ExotelClient{
		apiKey:     apiKey,
		apiToken:   apiToken,
		accountSID: accountSID,
		callerID:   callerID,
		appID:      appID,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

// InitiateCall dials toPhone via Exotel Connect API and returns the call SID.
// exomlURL is the URL Exotel fetches to get instructions (ExoML XML); when
// empty, falls back to the legacy Exotel-hosted app at http://my.exotel.com/exoml/start/{appID}.
// Pointing exomlURL at our own /webhook/exotel lets us forward per-call
// query params (lead name, phone, lead_id, …) into the WebSocket URL — without
// it, every call lands on /media-stream with no name and the WS handler is
// forced to guess from a racy Redis "latest" entry.
// callbackURL receives status events (answered, completed, etc.).
//
// Do NOT send "To" in app-based flow — Exotel rejects the combination of
// Url + To with 400 Bad/missing parameters (code 34001).
func (e *ExotelClient) InitiateCall(ctx context.Context, toPhone, exomlURL, callbackURL string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://api.exotel.com/v1/Accounts/%s/Calls/connect.json",
		e.accountSID)

	if exomlURL == "" {
		exomlURL = fmt.Sprintf("http://my.exotel.com/exoml/start/%s", e.appID)
	}

	phone := ExotelPhone(toPhone)
	form := url.Values{}
	form.Set("From", phone)
	form.Set("CallerId", e.callerID)
	form.Set("Url", exomlURL)
	form.Set("CallType", "trans")
	if callbackURL != "" {
		form.Set("StatusCallback", callbackURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("exotel: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(e.apiKey, e.apiToken)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exotel: dial: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("exotel: status %d: %s", resp.StatusCode, string(body))
	}
	// Response JSON: {"Call":{"Sid":"...","Status":"in-progress",...}}
	sid := extractNestedJSON(string(body), "Call", "Sid")
	if sid == "" {
		// Fallback: try top-level Sid
		sid = extractJSON(string(body), "Sid")
	}
	if sid == "" {
		return "", fmt.Errorf("exotel: no Sid in response: %s", string(body))
	}
	return sid, nil
}

// NormalizePhone converts an Indian phone number to E.164 format (+91XXXXXXXXXX).
// Handles 10-digit numbers, numbers with spaces/dashes, and numbers already with +91.
func NormalizePhone(phone string) string {
	// Strip whitespace, dashes, parentheses
	phone = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' || r == '.' {
			return -1
		}
		return r
	}, phone)
	if strings.HasPrefix(phone, "+91") {
		return phone
	}
	if strings.HasPrefix(phone, "91") && len(phone) == 12 {
		return "+" + phone
	}
	if len(phone) == 10 {
		return "+91" + phone
	}
	return phone
}

// ExotelPhone returns the phone in the format Exotel's Connect API accepts:
// "91XXXXXXXXXX" (country code + number, no leading +). 10-digit numbers get
// "91" prefixed. Matches the Python dial_exotel normalisation.
func ExotelPhone(phone string) string {
	phone = strings.TrimSpace(NormalizePhone(phone))
	phone = strings.TrimPrefix(phone, "+")
	if len(phone) == 10 {
		return "91" + phone
	}
	return phone
}
