package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/api"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := control.New(control.Config{
		LeaseTTL:           durationEnv("LEASE_TTL", 30*time.Second),
		WorkerTTL:          durationEnv("WORKER_TTL", 45*time.Second),
		DefaultTenantLimit: intEnv("DEFAULT_TENANT_LIMIT", 2),
		PreviewBaseDomain:  stringEnv("PREVIEW_BASE_DOMAIN", "preview.local"),
		PreviewDeleteDelay: durationEnv("PREVIEW_DELETE_DELAY", 5*time.Second),
	})
	handler := api.NewServer(store, os.Getenv("GITHUB_WEBHOOK_SECRET"), logger)
	server := &http.Server{
		Addr:              stringEnv("LISTEN_ADDR", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go runLoops(ctx, store, logger)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown failed", "error", err)
		}
	}()

	logger.Info("CI preview control plane started", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("CI preview control plane stopped")
}

func runLoops(ctx context.Context, store *control.Store, logger *slog.Logger) {
	scheduler := time.NewTicker(250 * time.Millisecond)
	reconciler := time.NewTicker(time.Second)
	defer scheduler.Stop()
	defer reconciler.Stop()
	for {
		select {
		case now := <-scheduler.C:
			for range 100 {
				lease, err := store.ScheduleOne(now)
				if errors.Is(err, control.ErrNoWork) {
					break
				}
				if err != nil {
					logger.Error("scheduler iteration failed", "error", err)
					break
				}
				logger.Info("job leased", "attempt_id", lease.Attempt.ID, "job_id", lease.Job.ID, "worker_id", lease.Attempt.WorkerID, "tenant", lease.Pipeline.Tenant)
			}
		case now := <-reconciler.C:
			expired, deleted := store.Reconcile(now)
			if expired > 0 || deleted > 0 {
				logger.Info("reconciliation completed", "expired_leases", expired, "deleted_previews", deleted)
			}
		case <-ctx.Done():
			return
		}
	}
}

func stringEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
