// Package wa handles WhatsApp multi-provider message parsing and sending.
package wa

import (
	"encoding/json"
	"strings"
)

// IncomingMessage is a normalised inbound WA message regardless of provider.
type IncomingMessage struct {
	ProviderMsgID string
	FromPhone     string
	ToPhone       string
	Text          string
	MediaURL      string
	MessageType   string // text, image, audio, document
	Provider      string
}

// ParseGupshup parses an inbound Gupshup webhook payload.
func ParseGupshup(body []byte) (*IncomingMessage, error) {
	var p struct {
		Type    string `json:"type"`
		Payload struct {
			ID      string `json:"id"`
			Source  string `json:"source"`
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Text string `json:"text"`
				URL  string `json:"url"`
			} `json:"payload"`
			Destination string `json:"destination"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	return &IncomingMessage{
		ProviderMsgID: p.Payload.ID,
		FromPhone:     p.Payload.Source,
		ToPhone:       p.Payload.Destination,
		Text:          p.Payload.Payload.Text,
		MediaURL:      p.Payload.Payload.URL,
		MessageType:   coalesce(p.Payload.Payload.Type, "text"),
		Provider:      "gupshup",
	}, nil
}

// ParseWati parses an inbound Wati webhook payload.
func ParseWati(body []byte) (*IncomingMessage, error) {
	var p struct {
		ID      string `json:"id"`
		WaID    string `json:"waId"`
		Type    string `json:"type"`
		Text    struct{ Body string } `json:"text"`
		Image   struct{ URL string }  `json:"image"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	text := p.Text.Body
	mediaURL := p.Image.URL
	msgType := p.Type
	if msgType == "" {
		msgType = "text"
	}
	return &IncomingMessage{
		ProviderMsgID: p.ID,
		FromPhone:     p.WaID,
		Text:          text,
		MediaURL:      mediaURL,
		MessageType:   msgType,
		Provider:      "wati",
	}, nil
}

// ParseInterakt parses an inbound Interakt webhook payload.
func ParseInterakt(body []byte) (*IncomingMessage, error) {
	var p struct {
		Data struct {
			Message struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Message struct{ Text string } `json:"message"`
			} `json:"message"`
			Customer struct{ PhoneNumber string } `json:"customer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	return &IncomingMessage{
		ProviderMsgID: p.Data.Message.ID,
		FromPhone:     p.Data.Customer.PhoneNumber,
		Text:          p.Data.Message.Message.Text,
		MessageType:   coalesce(p.Data.Message.Type, "text"),
		Provider:      "interakt",
	}, nil
}

// ParseMeta parses an inbound Meta (WhatsApp Business API) webhook payload.
func ParseMeta(body []byte) (*IncomingMessage, error) {
	var p struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						ID   string `json:"id"`
						From string `json:"from"`
						Type string `json:"type"`
						Text struct{ Body string } `json:"text"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				return &IncomingMessage{
					ProviderMsgID: msg.ID,
					FromPhone:     msg.From,
					Text:          msg.Text.Body,
					MessageType:   coalesce(msg.Type, "text"),
					Provider:      "meta",
				}, nil
			}
		}
	}
	return nil, nil
}

// ParseAiSensei parses an inbound AiSensei webhook payload.
// AiSensei uses the same format as Gupshup.
func ParseAiSensei(body []byte) (*IncomingMessage, error) {
	msg, err := ParseGupshup(body)
	if err == nil && msg != nil {
		msg.Provider = "aisensei"
	}
	return msg, err
}

// NormalizePhone ensures a phone number has a + prefix.
func NormalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}
	if !strings.HasPrefix(phone, "+") {
		// Assume India if 10 digits
		if len(phone) == 10 {
			return "+91" + phone
		}
		return "+" + phone
	}
	return phone
}

func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
