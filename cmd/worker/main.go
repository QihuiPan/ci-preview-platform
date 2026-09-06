package main

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/kube"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
	"github.com/QihuiPan/ci-preview-platform/internal/transport"
)

func main() {
	if len(os.Args) > 1 {
		if e := helper(os.Args[1]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		slog.Error("worker stopped", "error", e)
		os.Exit(1)
	}
}
func helper(mode string) error {
	switch mode {
	case "hold":
		time.Sleep(2 * time.Hour)
		return nil
	case "metadata":
		p := "/workspace/.ci-build-metadata.json"
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("invalid build metadata")
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		defer f.Close()
		_, e = io.CopyN(os.Stdout, f, info.Size())
		return e
	case "archive":
		return archive("/workspace/artifacts", os.Stdout)
	default:
		return errors.New("unknown helper command")
	}
}

// Archive exports only regular files, rejects symlinks and bounds total transfer size.
func archive(root string, dest io.Writer) error {
	writer := tar.NewWriter(dest)
	defer writer.Close()
	var size int64
	files := 0
	if _, e := os.Stat(root); os.IsNotExist(e) {
		return writer.Close()
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("artifact symlinks are forbidden")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("only regular artifact files are supported")
		}
		files++
		size += ((info.Size()+511)/512)*512 + 512
		if files > 2048 || size > (16<<20)-2048 {
			return errors.New("artifacts exceed the 16 MiB or 2048 file limit")
		}
		name, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		header, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		header.Name = filepath.ToSlash(name)
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if e = writer.WriteHeader(header); e != nil {
			return e
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		_, e = io.CopyN(writer, f, info.Size())
		return e
	})
}

type worker struct {
	client   *transport.Client
	runner   kube.Runner
	identity domain.Worker
	mu       sync.Mutex
	active   map[string]bool
	wait     sync.WaitGroup
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	client, e := transport.Env()
	if e != nil {
		return e
	}
	runner := kube.Runner{Image: os.Getenv("WORKER_IMAGE"), GitImage: os.Getenv("GIT_IMAGE"), BuildkitImage: os.Getenv("BUILDKIT_IMAGE")}
	if runner.Image == "" {
		return errors.New("WORKER_IMAGE is required")
	}
	if runner.GitImage == "" {
		runner.GitImage = "alpine/git:2.49.1"
	}
	if runner.BuildkitImage == "" {
		runner.BuildkitImage = "moby/buildkit:v0.33.0-rootless"
	}
	if path := os.Getenv("REGISTRY_CONFIG_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		runner.RegistryConfig = string(b)
	}
	w := &worker{client: client, runner: runner, active: map[string]bool{}}
	if e = client.Do(ctx, "POST", "/v1/workers/register", map[string]any{}, &w.identity); e != nil {
		return e
	}
	slog.Info("worker registered", "id", w.identity.ID, "trusted", w.identity.Trusted, "capacity", w.identity.Capacity)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	defer w.wait.Wait()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if e = client.Do(ctx, "POST", "/v1/workers/"+w.identity.ID+"/heartbeat", map[string]any{}, nil); e != nil {
				slog.Warn("worker heartbeat failed", "error", e)
				continue
			}
			var out struct {
				Assignments []domain.AttemptLease `json:"assignments"`
			}
			if e = client.Do(ctx, "GET", "/v1/workers/"+w.identity.ID+"/assignments", nil, &out); e != nil {
				slog.Warn("assignment polling failed", "error", e)
				continue
			}
			for _, l := range out.Assignments {
				w.mu.Lock()
				busy := w.active[l.Attempt.ID]
				if !busy && len(w.active) < w.identity.Capacity {
					w.active[l.Attempt.ID] = true
					w.wait.Add(1)
					go w.execute(ctx, l)
				}
				w.mu.Unlock()
			}
		}
	}
}
func (w *worker) execute(parent context.Context, l domain.AttemptLease) {
	defer w.wait.Done()
	defer func() { w.mu.Lock(); delete(w.active, l.Attempt.ID); w.mu.Unlock() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 45*time.Second)
		defer c()
		if e := w.runner.Client.DeleteNamespace(cleanup, kube.JobNamespace(l.Attempt.ID), l.Attempt.ID); e != nil {
			slog.Warn("namespace cleanup requires reconciliation", "attempt", l.Attempt.ID, "error", e)
		}
	}()
	heartbeat := func() error {
		return w.client.Do(ctx, "POST", "/v1/attempts/"+l.Attempt.ID+"/heartbeat", map[string]string{"lease_token": l.LeaseToken}, nil)
	}
	if e := heartbeat(); e != nil {
		return
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := heartbeat(); e != nil {
					slog.Warn("lease heartbeat rejected; stopping execution", "attempt", l.Attempt.ID, "error", e)
					cancel()
					return
				}
			}
		}
	}()
	var token struct {
		Token string `json:"token"`
	}
	if e := w.client.Raw(ctx, "GET", "/v1/attempts/"+l.Attempt.ID+"/source-token", nil, l.LeaseToken, &token); e != nil {
		slog.Warn("checkout authorization failed", "attempt", l.Attempt.ID, "error", e)
		return
	}
	result, err := w.runner.Execute(ctx, l, token.Token)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		result.Success = false
		result.Message = "Execution infrastructure error; inspect worker logs"
		slog.Warn("execution failed", "attempt", l.Attempt.ID, "error", err)
	}
	if result.Digest != "" && !planner.DigestPattern.MatchString(result.Digest) {
		result.Success = false
		result.Digest = ""
		result.Message = "Builder returned an invalid digest"
	}
	artifacts := []domain.Artefact{}
	logKey := ""
	upload := func(name string, data []byte) error {
		var artifact domain.Artefact
		if e := w.client.Raw(ctx, "PUT", "/v1/attempts/"+l.Attempt.ID+"/objects/"+name, data, l.LeaseToken, &artifact); e != nil {
			return e
		}
		if name == "logs" {
			logKey = artifact.Key
		} else {
			artifacts = append(artifacts, artifact)
		}
		return nil
	}
	if e := upload("logs", result.Logs); e != nil {
		slog.Warn("log persistence failed; lease will retry", "error", e)
		return
	}
	if len(result.Archive) > 0 {
		if e := upload("artifacts.tar", result.Archive); e != nil {
			return
		}
	}
	if len(result.Metadata) > 0 {
		if e := upload("build-metadata.json", result.Metadata); e != nil {
			return
		}
	}
	provenance, _ := json.Marshal(map[string]any{"schema_version": 1, "pipeline_id": l.Pipeline.ID, "attempt_id": l.Attempt.ID, "source_repository": l.Pipeline.SourceRepo, "commit_sha": l.Pipeline.CommitSHA, "configuration_digest": l.Pipeline.ConfigDigest, "image_digest": result.Digest, "worker_id": w.identity.ID, "trusted": w.identity.Trusted, "completed_at": time.Now().UTC(), "note": "Execution record; OCI attestations are emitted by BuildKit for image jobs"})
	if e := upload("provenance.json", provenance); e != nil {
		return
	}
	payload := map[string]any{"lease_token": l.LeaseToken, "success": result.Success, "message": strings.TrimSpace(result.Message), "digest": result.Digest, "log_key": logKey, "artefacts": artifacts}
	for range 3 {
		e := w.client.Do(ctx, "POST", "/v1/attempts/"+l.Attempt.ID+"/complete", payload, nil)
		if e == nil {
			slog.Info("attempt completed", "attempt", l.Attempt.ID, "success", result.Success)
			return
		}
		var httpErr *transport.HTTPError
		if errors.As(e, &httpErr) && httpErr.Status == 409 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
