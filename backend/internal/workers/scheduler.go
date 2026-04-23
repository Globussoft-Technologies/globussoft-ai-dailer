// Package workers provides background goroutines for scheduled calls, retries, and CRM polling.
package workers

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
)

// Scheduler polls the scheduled_calls table every 60 seconds and dials due calls.
type Scheduler struct {
	db        *db.DB
	initiator *dial.Initiator
	log       *zap.Logger
}

// NewScheduler creates a Scheduler.
func NewScheduler(database *db.DB, initiator *dial.Initiator, log *zap.Logger) *Scheduler {
	return &Scheduler{db: database, initiator: initiator, log: log}
}

// Run starts the scheduler loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	s.log.Info("scheduler: started")
	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler: stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	calls, err := s.db.GetPendingScheduledCalls()
	if err != nil {
		s.log.Warn("scheduler: GetPendingScheduledCalls", zap.Error(err))
		return
	}
	for _, sc := range calls {
		if ctx.Err() != nil {
			return
		}
		// Mark as processing immediately to avoid double-dial on next tick
		if err := s.db.UpdateScheduledCallStatus(sc.ID, "processing"); err != nil {
			s.log.Warn("scheduler: mark processing", zap.Error(err), zap.Int64("id", sc.ID))
			continue
		}

		lead, err := s.db.GetLeadByID(sc.LeadID)
		if err != nil || lead == nil {
			s.log.Warn("scheduler: lead not found", zap.Int64("lead_id", sc.LeadID))
			_ = s.db.UpdateScheduledCallStatus(sc.ID, "failed")
			continue
		}

		vs, _ := s.db.GetCampaignVoiceSettings(sc.CampaignID)
		data := dial.CallData{
			LeadID:      lead.ID,
			LeadName:    lead.FirstName + " " + lead.LastName,
			LeadPhone:   lead.Phone,
			CampaignID:  sc.CampaignID,
			OrgID:       sc.OrgID,
			Interest:    lead.Interest,
			TTSProvider: vs.TTSProvider,
			TTSVoiceID:  vs.TTSVoiceID,
			TTSLanguage: vs.TTSLanguage,
		}

		if err := s.initiator.Initiate(ctx, data); err != nil {
			s.log.Warn("scheduler: initiate failed",
				zap.Error(err), zap.Int64("scheduled_call_id", sc.ID))
			_ = s.db.UpdateScheduledCallStatus(sc.ID, "failed")
		} else {
			_ = s.db.UpdateScheduledCallStatus(sc.ID, "completed")
		}
	}
	if len(calls) > 0 {
		s.log.Info("scheduler: processed scheduled calls", zap.Int("count", len(calls)))
	}
}
