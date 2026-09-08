package workers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
	rstore "github.com/globussoft/callified-backend/internal/redis"
)

// DialerWorker consumes a Redis-backed queue of outbound calls and dials them
// sequentially with provider rate limits and retry logic.
type DialerWorker struct {
	db        *db.DB
	store     *rstore.Store
	initiator *dial.Initiator
	log       *zap.Logger
	// minGap between dials prevents carrier spam flags and aligns with the
	// existing 30-second frontend contract.
	minGap time.Duration
	// lastDial tracks the last time we invoked initiator.Initiate so the worker
	// enforces the gap even when retries are mixed in.
	lastDial time.Time
}

// NewDialerWorker creates a DialerWorker.
func NewDialerWorker(database *db.DB, store *rstore.Store, initiator *dial.Initiator, log *zap.Logger) *DialerWorker {
	return &DialerWorker{
		db:        database,
		store:     store,
		initiator: initiator,
		log:       log,
		minGap:    30 * time.Second,
	}
}

// Run starts the queue consumer loop. Blocks until ctx is cancelled.
func (w *DialerWorker) Run(ctx context.Context) {
	w.log.Info("dialer_worker: started", zap.Duration("min_gap", w.minGap))
	// Short initial sleep to avoid racing with the HTTP startup sequence.
	time.Sleep(2 * time.Second)
	for {
		if ctx.Err() != nil {
			w.log.Info("dialer_worker: stopped")
			return
		}
		if err := w.tick(ctx); err != nil {
			w.log.Warn("dialer_worker: tick error", zap.Error(err))
			time.Sleep(1 * time.Second)
		}
	}
}

// tick processes one job per invocation. It returns an error only for transient
// failures so the caller can retry; terminal errors are logged and swallowed.
func (w *DialerWorker) tick(ctx context.Context) error {
	// 1. Move due retry jobs back into the dial queue.
	if _, err := w.store.PollDialRetries(ctx, 100); err != nil {
		w.log.Warn("dialer_worker: retry poll failed", zap.Error(err))
	}

	// 2. Enforce the minimum gap between dials.
	if remaining := w.minGap - time.Since(w.lastDial); remaining > 0 {
		time.Sleep(remaining)
	}

	// 3. Fetch the next job. Use a short blocking pop so the loop is not busy.
	job, err := w.store.BlockingNextDialJob(ctx, 2*time.Second)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}

	// 4. Check campaign state.
	state, err := w.store.GetDialState(ctx, job.CampaignID)
	if err != nil {
		w.log.Warn("dialer_worker: failed to read dial state", zap.Int64("campaign_id", job.CampaignID), zap.Error(err))
	}
	if state.Aborted {
		w.log.Info("dialer_worker: discarding job from aborted campaign", zap.Int64("lead_id", job.LeadID), zap.Int64("campaign_id", job.CampaignID))
		return nil
	}
	if state.Paused {
		// Requeue at the head so the job is picked up immediately on resume.
		if err := w.requeueHead(ctx, job); err != nil {
			w.log.Warn("dialer_worker: requeue on pause failed", zap.Error(err))
		}
		// Sleep briefly to avoid busy-looping while paused.
		time.Sleep(1 * time.Second)
		return nil
	}
	w.lastDial = time.Now()
	w.processJob(ctx, job, state)
	return nil
}

func (w *DialerWorker) requeueHead(ctx context.Context, job *rstore.DialJob) error {
	return w.store.EnqueueDialJobs(ctx, rstore.DialState{}, []rstore.DialJob{*job})
}

func (w *DialerWorker) processJob(ctx context.Context, job *rstore.DialJob, state rstore.DialState) {
	// DND check before dialing.
	if job.OrgID > 0 {
		if isDND, _ := w.db.IsDNDNumber(job.OrgID, job.LeadPhone); isDND {
			w.store.EmitCampaignEvent(ctx, job.CampaignID, job.LeadName, job.LeadPhone, "dnd", "on DND list")
			w.store.PublishDomainEvent(ctx, rstore.DomainEvent{
				Type:       rstore.EventLeadStatusChanged,
				OrgID:      job.OrgID,
				CampaignID: job.CampaignID,
				LeadID:     job.LeadID,
				Status:     "DND — do not call",
			})
			w.updateState(ctx, job.CampaignID, func(s *rstore.DialState) {
				s.ProcessedCount++
			})
			_ = w.db.UpdateLeadStatus(job.LeadID, "DND — do not call")
			return
		}
	}

	data := dial.CallData{
		LeadID:          job.LeadID,
		LeadName:        job.LeadName,
		LeadPhone:       job.LeadPhone,
		CampaignID:      job.CampaignID,
		OrgID:           job.OrgID,
		Interest:        job.Interest,
		Language:        job.TTSLanguage,
		TTSProvider:     job.TTSProvider,
		TTSVoiceID:      job.TTSVoiceID,
		TTSLanguage:     job.TTSLanguage,
		UserEmail:       job.UserEmail,
		UserID:          job.UserID,
		ExotelAccountID: job.ExotelAccountID,
	}

	_, err := w.initiator.Initiate(ctx, data)
	if err == nil {
		w.store.PublishDomainEvent(ctx, rstore.DomainEvent{
			Type:       rstore.EventLeadStatusChanged,
			OrgID:      job.OrgID,
			CampaignID: job.CampaignID,
			LeadID:     job.LeadID,
			Status:     "Calling",
		})
		w.updateState(ctx, job.CampaignID, func(s *rstore.DialState) {
			s.ProcessedCount++
		})
		return
	}

	w.log.Warn("dialer_worker: dial failed",
		zap.Int64("lead_id", job.LeadID),
		zap.Int64("campaign_id", job.CampaignID),
		zap.Int("attempt", job.Attempt),
		zap.Error(err))

	job.LastError = err.Error()

	// Insufficient credits is not retryable for this org — stop the queue so we
	// don't churn through every remaining lead.
	if errors.Is(err, dial.ErrInsufficientCredits) {
		w.updateState(ctx, job.CampaignID, func(s *rstore.DialState) {
			s.Running = false
			s.Paused = false
			s.LastError = "insufficient credits — recharge to continue"
		})
		w.store.EmitCampaignEvent(ctx, job.CampaignID, "Campaign", "", "failed", "insufficient credits — recharge to continue")
		w.store.PublishDomainEvent(ctx, rstore.DomainEvent{
			Type:       rstore.EventCampaignDialFinished,
			OrgID:      job.OrgID,
			CampaignID: job.CampaignID,
			Status:     "stopped",
			Detail:     "insufficient credits — recharge to continue",
		})
		return
	}

	// Retryable failures get exponential backoff up to MaxAttempts.
	if job.Attempt < job.MaxAttempts {
		backoff := time.Duration(1<<job.Attempt) * time.Minute
		if backoff > 30*time.Minute {
			backoff = 30 * time.Minute
		}
		w.updateState(ctx, job.CampaignID, func(s *rstore.DialState) {
			s.RetryCount++
		})
		if rerr := w.store.EnqueueDialRetry(ctx, job, backoff); rerr != nil {
			w.log.Warn("dialer_worker: retry enqueue failed", zap.Error(rerr))
		}
		return
	}

	// Exhausted retries.
	w.updateState(ctx, job.CampaignID, func(s *rstore.DialState) {
		s.ProcessedCount++
		s.FailedCount++
		s.LastError = job.LastError
	})
	failedStatus := fmt.Sprintf("Call Failed (%d attempts)", job.Attempt)
	w.store.PublishDomainEvent(ctx, rstore.DomainEvent{
		Type:       rstore.EventLeadStatusChanged,
		OrgID:      job.OrgID,
		CampaignID: job.CampaignID,
		LeadID:     job.LeadID,
		Status:     failedStatus,
		Outcome:    job.LastError,
	})
	_ = w.db.UpdateLeadStatus(job.LeadID, failedStatus)
}

func (w *DialerWorker) updateState(ctx context.Context, campaignID int64, fn func(*rstore.DialState)) {
	state, err := w.store.GetDialState(ctx, campaignID)
	if err != nil {
		w.log.Warn("dialer_worker: failed to read state for update", zap.Int64("campaign_id", campaignID), zap.Error(err))
		return
	}
	fn(&state)
	if state.QueuedCount > 0 && state.ProcessedCount >= state.QueuedCount {
		state.Running = false
		state.Paused = false
		state.CompletedAt = time.Now()
		w.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "", "finished", fmt.Sprintf("Dial queue complete (%d leads)", state.QueuedCount))
		w.store.PublishDomainEvent(ctx, rstore.DomainEvent{
			Type:       rstore.EventCampaignDialFinished,
			OrgID:      state.CampaignID,
			CampaignID: state.CampaignID,
			Status:     "completed",
			Detail:     fmt.Sprintf("Dial queue complete (%d leads)", state.QueuedCount),
		})
	}
	if err := w.store.SetDialState(ctx, state); err != nil {
		w.log.Warn("dialer_worker: failed to write state", zap.Int64("campaign_id", campaignID), zap.Error(err))
	}
}
