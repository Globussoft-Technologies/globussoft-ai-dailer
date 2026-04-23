// Package dial provides Twilio and Exotel REST clients for initiating outbound calls.
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

// TwilioClient calls the Twilio REST API.
type TwilioClient struct {
	accountSID string
	authToken  string
	fromPhone  string
	client     *http.Client
}

// NewTwilioClient creates a Twilio REST client.
func NewTwilioClient(accountSID, authToken, fromPhone string) *TwilioClient {
	return &TwilioClient{
		accountSID: accountSID,
		authToken:  authToken,
		fromPhone:  fromPhone,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

// InitiateCall dials toPhone via Twilio and returns the call SID.
// twimlURL is the endpoint Twilio fetches to get TwiML instructions.
// statusCallbackURL receives call lifecycle events.
func (t *TwilioClient) InitiateCall(ctx context.Context, toPhone, twimlURL, statusCallbackURL string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json",
		t.accountSID)

	form := url.Values{}
	form.Set("To", toPhone)
	form.Set("From", t.fromPhone)
	form.Set("Url", twimlURL)
	form.Set("StatusCallback", statusCallbackURL)
	form.Set("StatusCallbackMethod", "POST")
	form.Set("StatusCallbackEvent", "initiated ringing answered completed")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("twilio: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.accountSID, t.authToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("twilio: dial: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("twilio: status %d: %s", resp.StatusCode, string(body))
	}
	// Extract Sid from JSON response: {"sid":"CA...","status":"queued",...}
	sid := extractJSON(string(body), "sid")
	if sid == "" {
		return "", fmt.Errorf("twilio: no sid in response: %s", string(body))
	}
	return sid, nil
}
