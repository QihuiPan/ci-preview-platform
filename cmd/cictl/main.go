package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/auth"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
	"github.com/QihuiPan/ci-preview-platform/internal/transport"
)

func main() {
	if e := run(); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func initialize(args []string) error {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	namespace := f.String("namespace", "ci-platform", "Kubernetes namespace")
	release := f.String("release", "ci", "Helm release name")
	repo := f.String("repo", "octocat/Hello-World", "Allowlisted GitHub repository")
	if e := f.Parse(args); e != nil {
		return e
	}
	valid := regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	if !valid.MatchString(*namespace) || !valid.MatchString(*release) || !planner.RepositoryPattern.MatchString(*repo) {
		return errors.New("invalid namespace, release or repository")
	}
	admin, tenant, worker, controller, password := random(), random(), random(), random(), random()
	config := auth.Config{Principals: []auth.Principal{{Token: admin, Role: "admin"}, {Token: tenant, Role: "tenant", Tenant: "default"}, {Token: worker, Role: "worker", Worker: &domain.Worker{ID: "worker-untrusted-1", Pool: "untrusted", Capacity: 2, Capabilities: map[string]bool{"linux-amd64": true}, Resources: domain.Resources{CPU: 4, Memory: 4096}}}, {Token: controller, Role: "controller"}}, Repositories: map[string]auth.Repository{*repo: {Tenant: "default", Trusted: false, AllowForks: false}}}
	b, _ := json.Marshal(config)
	values := map[string]string{"auth.json": string(b), "state-key": random(), "admin-token": admin, "tenant-token": tenant, "worker-token": worker, "controller-token": controller, "postgres-password": password, "database-url": fmt.Sprintf("postgres://ci:%s@%s-postgres:5432/ci?sslmode=disable", password, *release), "s3-access-key": "ci" + random()[:16], "s3-secret-key": random(), "webhook-secret": random()}
	data := map[string]string{}
	for k, v := range values {
		data[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": "ci-secrets", "namespace": *namespace}, "type": "Opaque", "data": data})
}
func run() error {
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-h" || arg == "help" {
			fmt.Println("cictl 0.2.0\nUsage: cictl init|submit|pipelines|show|cancel|previews|logs|artifact|version\nSubmit: --file CONFIG --repo OWNER/REPO --sha FULL_SHA [--pr NUMBER] [--key ID] [--rerun]\nRead: show PIPELINE_ID, logs ATTEMPT_ID, artifact ATTEMPT_ID NAME [--output FILE]\nConfigure API_URL and API_TOKEN or API_TOKEN_FILE for API commands.\nInit prints a new Kubernetes Secret; pipe it directly to kubectl create -f - once.")
			return nil
		}
	}
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("0.2.0")
		return nil
	}
	if len(os.Args) < 2 {
		return errors.New("usage: cictl init|submit|pipelines|show|cancel|previews|logs|artifact")
	}
	mode := os.Args[1]
	if mode == "init" {
		return initialize(os.Args[2:])
	}
	c, e := transport.Env()
	if e != nil {
		return e
	}
	method := "GET"
	path := ""
	var payload any
	key := ""
	rawOutput := false
	outputFile := ""
	switch mode {
	case "submit":
		f := flag.NewFlagSet("submit", flag.ContinueOnError)
		file := f.String("file", ".ci-preview.yml", "Pipeline YAML file")
		repo := f.String("repo", "", "Allowlisted GitHub repository")
		sha := f.String("sha", "", "Immutable 40-character commit SHA")
		pr := f.Int("pr", 0, "Pull request number for previews")
		requestID := f.String("key", "", "Stable retry idempotency key")
		rerun := f.Bool("rerun", false, "Create a new logical pipeline for an already built revision")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		b, e := os.ReadFile(*file)
		if e != nil {
			return e
		}
		spec, e := planner.Parse(b)
		if e != nil {
			return e
		}
		if !planner.RepositoryPattern.MatchString(*repo) || !planner.CommitPattern.MatchString(*sha) {
			return errors.New("--repo and --sha are required")
		}
		key = *requestID
		if key == "" {
			key = random()
			fmt.Fprintln(os.Stderr, "Idempotency key:", key)
		}
		payload = control.Submission{Repo: *repo, CommitSHA: *sha, PRNumber: *pr, Spec: spec, Rerun: *rerun}
		method = "POST"
		path = "/v1/pipelines"
	case "pipelines":
		path = "/v1/pipelines"
	case "previews":
		path = "/v1/previews"
	case "show", "cancel", "logs", "artifact":
		if len(os.Args) < 3 {
			return errors.New("resource ID is required")
		}
		id := os.Args[2]
		if strings.ContainsAny(id, "/?.") {
			return errors.New("invalid resource ID")
		}
		switch mode {
		case "show":
			path = "/v1/pipelines/" + id
		case "cancel":
			method = "POST"
			path = "/v1/pipelines/" + id + "/cancel"
		case "logs":
			path = "/v1/attempts/" + id + "/objects/logs"
			rawOutput = true
		case "artifact":
			if len(os.Args) < 4 || strings.ContainsAny(os.Args[3], "/?") {
				return errors.New("artifact requires an attempt ID and a valid artifact name")
			}
			f := flag.NewFlagSet("artifact", flag.ContinueOnError)
			f.StringVar(&outputFile, "output", "", "Save binary bytes to a new file without shell encoding conversions")
			if e = f.Parse(os.Args[4:]); e != nil {
				return e
			}
			if f.NArg() != 0 {
				return errors.New("unexpected artifact arguments")
			}
			path = "/v1/attempts/" + id + "/objects/" + os.Args[3]
			rawOutput = true
		}
	default:
		return errors.New("unknown command")
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, method, c.URL+path, strings.NewReader(string(body)))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	res, e := c.HTTP.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, b)
	}
	if rawOutput {
		if outputFile != "" {
			return saveArtifact(outputFile, res.Body)
		}
		_, e = io.Copy(os.Stdout, io.LimitReader(res.Body, 17<<20))
		return e
	}
	var out any
	if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
		return e
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func saveArtifact(path string, body io.Reader) error {
	data, e := io.ReadAll(io.LimitReader(body, (16<<20)+1))
	if e != nil {
		return e
	}
	if len(data) > 16<<20 {
		return errors.New("artifact exceeds 16 MiB")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		os.Remove(path)
	}
	return e
}
