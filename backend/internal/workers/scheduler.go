// Package workers provides background goroutines for scheduled calls, retries, and CRM polling.
package workers

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
)

// schedulerTickInterval is how often the scheduler polls for due calls.
const schedulerTickInterval = 1 * time.Second

// dialLeadTimeSeconds: how many seconds *before* the user-requested time we
// pull rows out of "pending" and dial them. The provider API call needs
// ~2–4 s to actually ring the phone (Twilio/Exotel handoff + telco routing),
// so kicking the dial off this many seconds early lets the phone ring at
// the exact second the user scheduled.
const dialLeadTimeSeconds = 3

// Scheduler polls the scheduled_calls table on a short interval and dials due calls.
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
	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()
	s.log.Info("scheduler: started", zap.Duration("interval", schedulerTickInterval))
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
	calls, err := s.db.GetPendingScheduledCalls(dialLeadTimeSeconds)
	if err != nil {
		s.log.Warn("scheduler: GetPendingScheduledCalls", zap.Error(err))
		return
	}
	for _, sc := range calls {
		if ctx.Err() != nil {
			return
		}
		// Mark as dialing immediately to avoid double-dial on next tick.
		// (Must be one of the scheduled_calls.status enum values:
		// pending|dialing|completed|failed|cancelled.)
		if err := s.db.UpdateScheduledCallStatus(sc.ID, "dialing"); err != nil {
			s.log.Warn("scheduler: mark dialing", zap.Error(err), zap.Int64("id", sc.ID))
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
			LeadID:                 lead.ID,
			LeadName:               lead.FirstName + " " + lead.LastName,
			LeadPhone:              lead.Phone,
			CampaignID:             sc.CampaignID,
			OrgID:                  sc.OrgID,
			Interest:               lead.Interest,
			TTSProvider:            vs.TTSProvider,
			TTSVoiceID:             vs.TTSVoiceID,
			TTSLanguage:            vs.TTSLanguage,
			MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
		}

		if _, err := s.initiator.Initiate(ctx, data); err != nil {
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
