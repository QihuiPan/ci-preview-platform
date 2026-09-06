package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

type client struct {
	baseURL  string
	workerID string
	http     *http.Client
	logger   *slog.Logger
	duration time.Duration
	mu       sync.Mutex
	active   map[string]bool
}

func main() {
	apiURL := flag.String("api", "http://localhost:8080", "Control-plane API URL")
	workerID := flag.String("id", "worker-local-1", "Stable worker identity")
	pool := flag.String("pool", "trusted", "Worker pool name")
	capabilities := flag.String("capabilities", "linux-amd64,buildkit-rootless", "Comma-separated capabilities")
	capacity := flag.Int("capacity", 2, "Maximum concurrent attempts")
	trusted := flag.Bool("trusted", true, "Allow jobs that require a trusted worker")
	duration := flag.Duration("job-duration", 2*time.Second, "Simulated isolated job duration")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	c := &client{
		baseURL: strings.TrimRight(*apiURL, "/"), workerID: *workerID,
		http: &http.Client{Timeout: 10 * time.Second}, logger: logger, duration: *duration,
		active: make(map[string]bool),
	}
	registration := map[string]any{
		"id": *workerID, "pool": *pool, "capabilities": splitCapabilities(*capabilities),
		"capacity": *capacity, "trusted": *trusted,
	}
	if err := c.post(ctx, "/v1/workers/register", registration, nil); err != nil {
		logger.Error("worker registration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("worker registered", "worker_id", *workerID, "pool", *pool, "capacity", *capacity)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.post(ctx, "/v1/workers/"+*workerID+"/heartbeat", map[string]any{}, nil); err != nil {
				logger.Warn("worker heartbeat failed", "error", err)
			}
			if err := c.poll(ctx); err != nil {
				logger.Warn("assignment poll failed", "error", err)
			}
		case <-ctx.Done():
			logger.Info("worker stopped")
			return
		}
	}
}

func (c *client) poll(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/workers/"+c.workerID+"/assignments", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("assignment request returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Assignments []domain.AttemptLease `json:"assignments"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return err
	}
	for _, assignment := range payload.Assignments {
		c.mu.Lock()
		alreadyActive := c.active[assignment.Attempt.ID]
		if !alreadyActive {
			c.active[assignment.Attempt.ID] = true
		}
		c.mu.Unlock()
		if !alreadyActive {
			go c.run(ctx, assignment)
		}
	}
	return nil
}

func (c *client) run(ctx context.Context, lease domain.AttemptLease) {
	defer func() {
		c.mu.Lock()
		delete(c.active, lease.Attempt.ID)
		c.mu.Unlock()
	}()
	c.logger.Info("simulated isolated job started", "attempt_id", lease.Attempt.ID, "job", lease.Job.Name)
	if err := c.post(ctx, "/v1/attempts/"+lease.Attempt.ID+"/heartbeat", map[string]any{"lease_token": lease.LeaseToken}, nil); err != nil {
		c.logger.Warn("attempt heartbeat rejected", "attempt_id", lease.Attempt.ID, "error", err)
		return
	}
	select {
	case <-time.After(c.duration):
	case <-ctx.Done():
		return
	}
	completion := map[string]any{"lease_token": lease.LeaseToken, "success": true, "message": "simulated isolated execution completed"}
	if err := c.post(ctx, "/v1/attempts/"+lease.Attempt.ID+"/complete", completion, nil); err != nil {
		c.logger.Warn("attempt completion rejected", "attempt_id", lease.Attempt.ID, "error", err)
		return
	}
	c.logger.Info("simulated isolated job completed", "attempt_id", lease.Attempt.ID, "job", lease.Job.Name)
}

func (c *client) post(ctx context.Context, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("request returned %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}
	if target != nil {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return nil
}

func splitCapabilities(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if capability := strings.TrimSpace(part); capability != "" {
			result = append(result, capability)
		}
	}
	return result
}
