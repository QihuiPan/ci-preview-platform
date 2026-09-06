package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/kube"
	"github.com/QihuiPan/ci-preview-platform/internal/transport"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	client, err := transport.Env()
	if err != nil {
		return err
	}
	controller := kube.PreviewController{Domain: os.Getenv("PREVIEW_DOMAIN"), IngressClass: os.Getenv("INGRESS_CLASS"), TLSSecret: os.Getenv("PREVIEW_TLS_SECRET")}
	if controller.IngressClass == "" {
		controller.IngressClass = "nginx"
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var out struct {
				Previews []domain.Preview `json:"previews"`
			}
			if e := client.Do(ctx, "GET", "/v1/internal/previews", nil, &out); e != nil {
				slog.Warn("preview list failed", "error", e)
				continue
			}
			for _, p := range out.Previews {
				actual, url, e := controller.Reconcile(ctx, p)
				message := ""
				if e != nil {
					message = "Kubernetes reconciliation failed; inspect controller logs"
					slog.Warn("preview reconciliation failed", "namespace", p.Namespace, "error", e)
				}
				if actual == p.Actual && url == p.URL && message == p.LastError {
					continue
				}
				if err := client.Do(ctx, "POST", "/v1/internal/previews/observe", map[string]any{"preview": p, "actual": actual, "url": url, "error": message}, nil); err != nil {
					slog.Warn("preview observation rejected", "namespace", p.Namespace, "error", err)
				}
			}
			if e := cleanup(ctx, client, controller.Client); e != nil {
				slog.Warn("orphan cleanup failed", "error", e)
			}
		}
	}
}
func cleanup(ctx context.Context, api *transport.Client, k kubectlClient) error {
	var active struct {
		IDs []string `json:"attempt_ids"`
	}
	if e := api.Do(ctx, "GET", "/v1/internal/active-attempts", nil, &active); e != nil {
		return e
	}
	keep := map[string]bool{}
	for _, id := range active.IDs {
		keep[id] = true
	}
	raw, e := k.Run(ctx, nil, "get", "namespaces", "-l", "ci-preview/kind=job", "-o", "json")
	if e != nil {
		return e
	}
	var namespaces struct {
		Items []struct {
			Metadata struct {
				Name    string            `json:"name"`
				Labels  map[string]string `json:"labels"`
				Created time.Time         `json:"creationTimestamp"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if e = json.Unmarshal(raw, &namespaces); e != nil {
		return e
	}
	for _, n := range namespaces.Items {
		owner := n.Metadata.Labels["ci-preview/owner"]
		if owner != "" && !keep[owner] && time.Since(n.Metadata.Created) > 90*time.Second {
			if e = k.DeleteNamespace(ctx, n.Metadata.Name, owner); e != nil {
				return e
			}
		}
	}
	return nil
}

type kubectlClient interface {
	Run(context.Context, []byte, ...string) ([]byte, error)
	DeleteNamespace(context.Context, string, string) error
}
