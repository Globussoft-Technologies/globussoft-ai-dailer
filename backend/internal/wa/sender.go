package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChannelConfig holds the credentials needed to send via a provider.
type ChannelConfig struct {
	Provider    string
	PhoneNumber string
	APIKey      string
	AppID       string // Gupshup source phone / Wati API URL prefix
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// SendText sends a plain text message via the provider configured in cfg.
func SendText(ctx context.Context, cfg ChannelConfig, toPhone, text string) error {
	switch cfg.Provider {
	case "gupshup":
		return sendGupshupText(ctx, cfg, toPhone, text)
	case "wati":
		return sendWatiText(ctx, cfg, toPhone, text)
	case "interakt":
		return sendInteraktText(ctx, cfg, toPhone, text)
	case "meta":
		return sendMetaText(ctx, cfg, toPhone, text)
	case "aisensei":
		return sendGupshupText(ctx, cfg, toPhone, text) // same API
	default:
		return fmt.Errorf("unknown WA provider: %s", cfg.Provider)
	}
}

func sendGupshupText(ctx context.Context, cfg ChannelConfig, toPhone, text string) error {
	payload := map[string]string{
		"channel":  "whatsapp",
		"source":   cfg.PhoneNumber,
		"destination": toPhone,
		"message":  fmt.Sprintf(`{"type":"text","text":"%s"}`, escapeJSON(text)),
		"src.name": cfg.AppID,
	}
	return doFormPost(ctx,
		"https://api.gupshup.io/sm/api/v1/msg",
		map[string]string{"apikey": cfg.APIKey},
		payload)
}

func sendWatiText(ctx context.Context, cfg ChannelConfig, toPhone, text string) error {
	// Wati REST: POST {apiURL}/api/v1/sendSessionMessage/{phone}
	apiURL := cfg.AppID
	if apiURL == "" {
		apiURL = "https://live-mt-server.wati.io"
	}
	u := fmt.Sprintf("%s/api/v1/sendSessionMessage/%s", strings.TrimRight(apiURL, "/"), toPhone)
	body, _ := json.Marshal(map[string]string{"messageText": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	return doRequest(req)
}

func sendInteraktText(ctx context.Context, cfg ChannelConfig, toPhone, text string) error {
	body, _ := json.Marshal(map[string]any{
		"countryCode": "+91",
		"phoneNumber": toPhone,
		"type":        "text",
		"data":        map[string]string{"message": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.interakt.ai/v1/public/message/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+cfg.APIKey)
	return doRequest(req)
}

func sendMetaText(ctx context.Context, cfg ChannelConfig, toPhone, text string) error {
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                toPhone,
		"type":              "text",
		"text":              map[string]string{"body": text},
	})
	u := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", cfg.PhoneNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	return doRequest(req)
}

func doFormPost(ctx context.Context, url string, headers, fields map[string]string) error {
	var buf bytes.Buffer
	first := true
	for k, v := range fields {
		if !first {
			buf.WriteByte('&')
		}
		buf.WriteString(k + "=" + v)
		first = false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}

func doRequest(req *http.Request) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WA send: HTTP %d — %s", resp.StatusCode, string(body))
	}
	return nil
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
