package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// GET /api/events
// @Summary     Domain events stream (SSE)
// @Description Server-sent event stream for structured domain events: lead status changes, call completions, agent presence, etc. Authenticate via Authorization header or ?ticket= SSE ticket.
// @Tags        sse
// @Produce     text/event-stream
// @Security    BearerAuth
// @Param       ticket  query  string  false  "Short-lived SSE ticket"
// @Success     200  {string}  string  "event: LEAD_STATUS_CHANGED\\ndata: {...}\\n\\n"
// @Router      /api/events [get]
func (s *Server) domainEventsSSE(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if ac.Email == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

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

	// Org-scoped users only receive events for their org; super-admins can
	// optionally subscribe to the global firehose by passing org_id=0.
	orgID := ac.OrgID
	if s.isSuperAdmin(ac.Email) && r.URL.Query().Get("org_id") == "0" {
		orgID = 0
	}

	channels := []string{"events:all"}
	if orgID > 0 {
		channels = append(channels, fmt.Sprintf("events:org:%d", orgID))
	}

	// Merge multiple Redis subscriptions into one channel. If Redis is down,
	// the merge goroutine exits immediately and the loop falls back to heartbeats.
	msgs := s.mergeSubscriptions(ctx, channels...)

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
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// mergeSubscriptions combines multiple Redis subscription channels into one Go
// channel. It closes the output channel when the context is done or all inputs
// close. If Redis is unavailable, the returned channel is closed immediately.
func (s *Server) mergeSubscriptions(ctx context.Context, channels ...string) <-chan string {
	out := make(chan string, 64)
	if s.store == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for _, ch := range channels {
			ch := ch
			in := s.store.Subscribe(ctx, ch)
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case msg, ok := <-in:
						if !ok {
							return
						}
						select {
						case out <- msg:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}
		<-ctx.Done()
	}()
	return out
}

