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
// callbackURL receives status events (answered, completed, etc.).
func (e *ExotelClient) InitiateCall(ctx context.Context, toPhone, callbackURL string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://api.exotel.com/v1/Accounts/%s/Calls/connect.json",
		e.accountSID)

	phone := NormalizePhone(toPhone)
	form := url.Values{}
	form.Set("From", phone)
	form.Set("To", e.callerID)
	form.Set("CallerId", e.callerID)
	form.Set("Url", fmt.Sprintf("http://my.exotel.com/%s/exoml/start/%s", e.accountSID, e.appID))
	form.Set("StatusCallback", callbackURL)
	form.Set("Record", "true")
	form.Set("RecordingChannels", "dual")

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
