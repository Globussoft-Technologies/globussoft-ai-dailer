package api

import (
	"fmt"
	"net/http"
	"time"
)

// GET /api/sse/live-logs  — SSE stream of live log entries from Redis pub/sub
func (s *Server) liveLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send initial ping
	fmt.Fprint(w, "event: ping\ndata: connected\n\n")
	flusher.Flush()

	// Subscribe to live-logs channel
	ctx := r.Context()
	msgs := s.store.Subscribe(ctx, "live-logs")

	// Heartbeat ticker to keep connection alive through load balancers
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// GET /api/campaign-events?campaign_id={id}  — frontend-facing alias
func (s *Server) campaignEventsQuery(w http.ResponseWriter, r *http.Request) {
	campaignID := r.URL.Query().Get("campaign_id")
	if campaignID == "" || campaignID == "0" {
		campaignID = "all"
	}
	s.streamCampaignEvents(w, r, campaignID)
}

// GET /api/sse/campaign/{id}/events  — SSE stream for campaign dial progress
func (s *Server) campaignEvents(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("id")
	s.streamCampaignEvents(w, r, campaignID)
}

func (s *Server) streamCampaignEvents(w http.ResponseWriter, r *http.Request, campaignID string) {

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	fmt.Fprint(w, "event: ping\ndata: connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	msgs := s.store.Subscribe(ctx, "campaign:"+campaignID)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: campaign\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
