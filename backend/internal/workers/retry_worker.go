package workers

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
)

// RetryWorker polls the call_retries table every 2 minutes and re-dials pending retries.
type RetryWorker struct {
	db        *db.DB
	initiator *dial.Initiator
	log       *zap.Logger
}

// NewRetryWorker creates a RetryWorker.
func NewRetryWorker(database *db.DB, initiator *dial.Initiator, log *zap.Logger) *RetryWorker {
	return &RetryWorker{db: database, initiator: initiator, log: log}
}

// Run starts the retry loop. Blocks until ctx is cancelled.
func (rw *RetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	rw.log.Info("retry_worker: started")
	for {
		select {
		case <-ctx.Done():
			rw.log.Info("retry_worker: stopped")
			return
		case <-ticker.C:
			rw.tick(ctx)
		}
	}
}

func (rw *RetryWorker) tick(ctx context.Context) {
	retries, err := rw.db.GetPendingRetries()
	if err != nil {
		rw.log.Warn("retry_worker: GetPendingRetries", zap.Error(err))
		return
	}
	for _, r := range retries {
		if ctx.Err() != nil {
			return
		}
		lead, err := rw.db.GetLeadByID(r.LeadID)
		if err != nil || lead == nil {
			rw.log.Warn("retry_worker: lead not found", zap.Int64("lead_id", r.LeadID))
			_ = rw.db.UpdateRetryStatus(r.ID, "exhausted")
			continue
		}

		vs, _ := rw.db.GetCampaignVoiceSettings(r.CampaignID)
		data := dial.CallData{
			LeadID:      lead.ID,
			LeadName:    lead.FirstName + " " + lead.LastName,
			LeadPhone:   lead.Phone,
			CampaignID:  r.CampaignID,
			OrgID:       r.OrgID,
			Interest:    lead.Interest,
			TTSProvider: vs.TTSProvider,
			TTSVoiceID:  vs.TTSVoiceID,
			TTSLanguage: vs.TTSLanguage,
		}

		if err := rw.initiator.Initiate(ctx, data); err != nil {
			rw.log.Warn("retry_worker: initiate failed",
				zap.Error(err), zap.Int64("retry_id", r.ID))
			exhausted, _ := rw.db.IncrRetryAttempt(r.ID)
			if exhausted {
				rw.log.Info("retry_worker: exhausted", zap.Int64("lead_id", r.LeadID))
			}
		} else {
			_ = rw.db.UpdateRetryStatus(r.ID, "completed")
		}
	}
	if len(retries) > 0 {
		rw.log.Info("retry_worker: processed retries", zap.Int("count", len(retries)))
	}
}
