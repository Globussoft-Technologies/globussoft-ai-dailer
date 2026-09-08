package redis

import (
	"context"
	"encoding/json"
	"fmt"
)

// DomainEventType is the kind of a structured domain event pushed over SSE.
type DomainEventType string

const (
	EventLeadStatusChanged    DomainEventType = "LEAD_STATUS_CHANGED"
	EventCallCompleted        DomainEventType = "CALL_COMPLETED"
	EventAgentPresenceChanged DomainEventType = "AGENT_PRESENCE_CHANGED"
	EventCampaignDialStarted  DomainEventType = "CAMPAIGN_DIAL_STARTED"
	EventCampaignDialFinished DomainEventType = "CAMPAIGN_DIAL_FINISHED"
)

// DomainEvent is a structured event published on Redis pub/sub and streamed to
// connected browsers via SSE.
type DomainEvent struct {
	Type       DomainEventType `json:"type"`
	OrgID      int64           `json:"org_id,omitempty"`
	CampaignID int64           `json:"campaign_id,omitempty"`
	LeadID     int64           `json:"lead_id,omitempty"`
	UserID     int64           `json:"user_id,omitempty"`
	Status     string          `json:"status,omitempty"`
	Outcome    string          `json:"outcome,omitempty"`
	Duration   int64           `json:"duration,omitempty"`
	Detail     string          `json:"detail,omitempty"`
}

// PublishDomainEvent publishes an event to both the org-scoped and global
// event channels. Subscribers to either channel receive the event.
func (s *Store) PublishDomainEvent(ctx context.Context, ev DomainEvent) {
	if s.rdb == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	msg := string(b)
	_ = s.rdb.Publish(ctx, key("events", "all"), msg).Err()
	if ev.OrgID > 0 {
		_ = s.rdb.Publish(ctx, key("events", fmt.Sprintf("org:%d", ev.OrgID)), msg).Err()
	}
}
