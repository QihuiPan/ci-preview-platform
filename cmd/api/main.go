package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/api"
	"github.com/QihuiPan/ci-preview-platform/internal/auth"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	githubapp "github.com/QihuiPan/ci-preview-platform/internal/github"
	"github.com/QihuiPan/ci-preview-platform/internal/objectstore"
	"github.com/QihuiPan/ci-preview-platform/internal/persistence"
)

func main() {
	if err := run(); err != nil {
		slog.Error("control plane stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	policy, err := auth.Load(os.Getenv("AUTH_CONFIG_FILE"))
	if err != nil {
		return err
	}
	key, err := hex.DecodeString(secret("STATE_KEY"))
	if err != nil {
		return errors.New("STATE_KEY must be hexadecimal")
	}
	config := control.Config{LeaseTTL: 30 * time.Second, WorkerTTL: 45 * time.Second, DefaultTenantLimit: 2, TenantCPU: 16, TenantMemoryMB: 32768, MaxQueuedJobs: 1000}
	backend, err := persistence.Open(ctx, secret("DATABASE_URL"), key, config)
	if err != nil {
		return err
	}
	defer backend.Close()
	objects := &objectstore.S3{Endpoint: os.Getenv("S3_ENDPOINT"), Bucket: os.Getenv("S3_BUCKET"), Region: os.Getenv("S3_REGION"), AccessKey: secret("S3_ACCESS_KEY"), SecretKey: secret("S3_SECRET_KEY")}
	if err = objects.Validate(); err != nil {
		return err
	}
	var app *githubapp.App
	if name := os.Getenv("GITHUB_APP_KEY_FILE"); name != "" {
		pem, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		id, e := strconv.ParseInt(os.Getenv("GITHUB_APP_ID"), 10, 64)
		if e != nil || id < 1 {
			return errors.New("GITHUB_APP_ID must be positive")
		}
		app, e = githubapp.NewApp(id, pem)
		if e != nil {
			return e
		}
	}
	handler := api.New(api.Options{Backend: backend, Auth: policy, Objects: objects, GitHub: app, WebhookSecret: secret("GITHUB_WEBHOOK_SECRET")})
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go loops(ctx, backend, app)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("control plane ready to serve", "address", address)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func secret(name string) string {
	if path := os.Getenv(name + "_FILE"); path != "" {
		data, e := os.ReadFile(path)
		if e != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	return os.Getenv(name)
}
func loops(ctx context.Context, backend persistence.Backend, app *githubapp.App) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	check := time.NewTicker(10 * time.Second)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			err := backend.Transact(ctx, true, "", func(st *control.Store, now time.Time) error {
				st.Reconcile(now)
				st.EnsurePreviews(now)
				for range 64 {
					_, e := st.ScheduleOne(now)
					if errors.Is(e, control.ErrNoWork) {
						break
					}
					if e != nil {
						return e
					}
				}
				return nil
			})
			if err != nil && ctx.Err() == nil {
				slog.Error("scheduler transaction failed", "error", err)
			}
		case <-check.C:
			if app == nil {
				continue
			}
			var list []domain.Pipeline
			if err := backend.Transact(ctx, false, "", func(st *control.Store, _ time.Time) error { list = st.PipelineList(""); return nil }); err != nil {
				continue
			}
			for _, p := range list {
				if p.InstallationID == 0 || p.CheckState == p.Status {
					continue
				}
				id, e := app.Check(ctx, p)
				if e != nil {
					slog.Warn("GitHub check update failed", "pipeline", p.ID, "error", e)
					continue
				}
				_ = backend.Transact(ctx, true, "github check update", func(st *control.Store, _ time.Time) error { return st.RecordCheck(p.ID, id, p.Status) })
			}
		}
	}
}
