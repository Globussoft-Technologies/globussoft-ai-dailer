// audiod is the Go audio service entry point.
// Handles WebSocket audio pipeline + REST API. No Python/gRPC dependency (Phase 4).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/api"
	"github.com/globussoft/callified-backend/internal/config"
	apidb "github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/prompt"
	"github.com/globussoft/callified-backend/internal/rag"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/recording"
	"github.com/globussoft/callified-backend/internal/wa"
	"github.com/globussoft/callified-backend/internal/webhook"
	"github.com/globussoft/callified-backend/internal/workers"
	"github.com/globussoft/callified-backend/internal/wshandler"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Redis store (in-memory fallback if Redis unavailable)
	store := rstore.New(cfg.RedisURL, logger)

	// MySQL database
	database, dbErr := apidb.New(cfg.DSN())
	if dbErr != nil {
		logger.Warn("MySQL unavailable — REST API endpoints and recording analysis disabled", zap.Error(dbErr))
	} else {
		defer database.Close()
	}

	// LLM provider (Phase 0 / Phase 4)
	llmProvider := llm.NewProvider(cfg, logger)

	// Prompt builder + recording service (Phase 3C / Phase 4)
	var promptBuilder *prompt.Builder
	var recordingSvc *recording.Service
	if database != nil {
		promptBuilder = prompt.NewBuilder(database)
		disp := webhook.New(database, logger)
		recordingSvc = recording.New(database, llmProvider, disp, cfg, logger)
	}

	// WebSocket handler — shared across /media-stream and /ws/sandbox
	wsHandler := wshandler.New(cfg, promptBuilder, recordingSvc, store, logger)

	mux := http.NewServeMux()

	// Exotel VoiceBot WebSocket endpoint
	mux.Handle("/media-stream", wsHandler)

	// Browser simulator WebSocket endpoint (dev/QA)
	mux.Handle("/ws/sandbox", wsHandler)

	// Manager monitor WebSocket: /ws/monitor/{stream_sid}
	// Managers connect here to receive live transcripts, inject whispers, trigger takeover.
	mux.HandleFunc("/ws/monitor/", wsHandler.ServeMonitor)

	// Dial initiator (Phase 2) — shares dispatcher with recording service
	var initiator *dial.Initiator
	if database != nil && recordingSvc != nil {
		// re-use the webhook dispatcher already created above for recording
		initiator = dial.New(cfg, store, database, webhook.New(database, logger), logger)
	}

	// RAG client + WA agent (Phase 3C)
	ragClient := rag.New(cfg.RAGServiceURL, logger)

	// REST API — mounted only when MySQL is available
	var apiServer *api.Server
	if database != nil {
		apiServer = api.New(database, cfg, store, initiator, llmProvider, logger)
		waAgent := wa.NewAgent(database, llmProvider, ragClient, logger)
		apiServer.SetWAAgent(waAgent)
		apiServer.RegisterRoutes(mux)
		logger.Info("REST API endpoints registered")
	}

	// Prometheus metrics endpoint (scraped by Prometheus/Grafana)
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"go-audio"}`)) //nolint:errcheck
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
		// No read/write timeout — WebSocket connections are long-lived
		IdleTimeout: 120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	// Worker context — cancelled on SIGTERM/SIGINT
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	// Background workers (Phase 2) — only when DB is available
	if database != nil && initiator != nil {
		go workers.NewScheduler(database, initiator, logger).Run(workerCtx)
		go workers.NewRetryWorker(database, initiator, logger).Run(workerCtx)
		go workers.NewCRMPoller(database, logger).Run(workerCtx)
	}

	go func() {
		logger.Info("Go audio service listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP server error", zap.Error(err))
		}
	}()

	<-quit
	workerCancel() // stop background workers
	logger.Info("shutting down (waiting for active calls to drain)")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Error("shutdown error", zap.Error(err))
	}
	logger.Info("shutdown complete")
}
