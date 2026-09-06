package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

type Runner struct {
	Client                                         Client
	Image, GitImage, BuildkitImage, RegistryConfig string
}

func (r Runner) Prepare(ctx context.Context, l domain.AttemptLease, token string) error {
	if l.Job.Spec.Buildkit != nil && !l.Job.Spec.Trusted {
		return errors.New("untrusted image publishing is forbidden")
	}
	ns := JobNamespace(l.Attempt.ID)
	owner := l.Attempt.ID
	if _, e := r.Client.OwnedNamespace(ctx, ns, owner); e != nil {
		return e
	}
	if e := r.Client.Apply(ctx, Namespace(ns, owner, "job", l.Job.Spec.Buildkit == nil)); e != nil {
		return e
	}
	resources := Boundaries(ns, 0)
	if token != "" {
		resources = append(resources, Resource("v1", "Secret", "checkout", ns, Object{"type": "Opaque", "data": Object{"header": base64.StdEncoding.EncodeToString([]byte("AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))))}}))
	}
	if l.Job.Spec.Buildkit != nil && r.RegistryConfig != "" {
		if !l.Job.Spec.Trusted {
			return errors.New("untrusted image publishing is forbidden")
		}
		resources = append(resources, Resource("v1", "Secret", "registry", ns, Object{"type": "Opaque", "data": Object{"config.json": base64.StdEncoding.EncodeToString([]byte(r.RegistryConfig))}}))
	}
	if e := r.Client.Apply(ctx, resources...); e != nil {
		return e
	}
	return r.Client.Apply(ctx, r.Pod(l, token != ""))
}
func (r Runner) Pod(l domain.AttemptLease, authenticated bool) Object {
	ns := JobNamespace(l.Attempt.ID)
	spec := l.Job.Spec
	timeout := spec.TimeoutSeconds
	if timeout == 0 {
		timeout = 900
	}
	volumes := []Object{{"name": "workspace", "emptyDir": Object{"sizeLimit": "10Gi"}}, {"name": "tmp", "emptyDir": Object{"sizeLimit": "1Gi"}}, {"name": "builder", "emptyDir": Object{"sizeLimit": "8Gi"}}}
	mounts := []Object{{"name": "workspace", "mountPath": "/workspace"}, {"name": "tmp", "mountPath": "/tmp"}}
	source := l.Pipeline.SourceRepo
	if source == "" {
		source = l.Pipeline.Repo
	}
	env := []Object{{"name": "SOURCE_REPO", "value": source}, {"name": "COMMIT_SHA", "value": l.Pipeline.CommitSHA}, {"name": "HOME", "value": "/tmp"}, {"name": "GIT_TERMINAL_PROMPT", "value": "0"}}
	if authenticated {
		env = append(env, Object{"name": "GIT_CONFIG_COUNT", "value": "1"}, Object{"name": "GIT_CONFIG_KEY_0", "value": "http.https://github.com/.extraheader"}, Object{"name": "GIT_CONFIG_VALUE_0", "valueFrom": Object{"secretKeyRef": Object{"name": "checkout", "key": "header"}}})
	}
	small := Object{"requests": Object{"cpu": "100m", "memory": "64Mi", "ephemeral-storage": "64Mi"}, "limits": Object{"cpu": "250m", "memory": "128Mi", "ephemeral-storage": "256Mi"}}
	init := Object{"name": "checkout", "image": r.GitImage, "command": []string{"/bin/sh", "-ec", `git init /workspace; cd /workspace; git remote add origin "https://github.com/${SOURCE_REPO}.git"; git fetch --depth=1 origin "$COMMIT_SHA"; git checkout --detach FETCH_HEAD; test "$(git rev-parse HEAD)" = "$COMMIT_SHA"`}, "env": env, "volumeMounts": mounts, "securityContext": Security(), "resources": small}
	job := Object{"name": "job", "image": spec.Image, "command": spec.Command, "workingDir": "/workspace", "volumeMounts": mounts, "env": []Object{{"name": "HOME", "value": "/tmp"}, {"name": "CI", "value": "true"}}, "securityContext": Security(), "resources": Object{"requests": Object{"cpu": strconv.Itoa(spec.Resources.CPU), "memory": fmt.Sprintf("%dMi", spec.Resources.Memory), "ephemeral-storage": "1Gi"}, "limits": Object{"cpu": strconv.Itoa(spec.Resources.CPU), "memory": fmt.Sprintf("%dMi", spec.Resources.Memory), "ephemeral-storage": "10Gi"}}}
	if b := spec.Buildkit; b != nil {
		job["image"] = r.BuildkitImage
		job["command"] = []string{"buildctl-daemonless.sh", "build", "--frontend", "dockerfile.v0", "--local", "context=" + path.Join("/workspace", b.Context), "--local", "dockerfile=" + path.Dir(path.Join("/workspace", b.Dockerfile)), "--opt", "filename=" + path.Base(b.Dockerfile), "--opt", "attest:sbom=", "--opt", "attest:provenance=mode=max", "--output", "type=image,name=" + b.Destination + ",push=true", "--metadata-file", "/workspace/.ci-build-metadata.json"}
		security := Security()
		security["readOnlyRootFilesystem"] = false
		security["allowPrivilegeEscalation"] = true
		security["capabilities"] = Object{"drop": []string{"ALL"}, "add": []string{"SETUID", "SETGID"}}
		security["seccompProfile"] = Object{"type": "Unconfined"}
		security["appArmorProfile"] = Object{"type": "Unconfined"}
		job["securityContext"] = security
		job["env"] = []Object{{"name": "BUILDKITD_FLAGS", "value": "--oci-worker-no-process-sandbox"}, {"name": "DOCKER_CONFIG", "value": "/registry"}, {"name": "HOME", "value": "/home/user"}}
		mounts = append(mounts, Object{"name": "builder", "mountPath": "/home/user/.local/share/buildkit"})
		if r.RegistryConfig != "" {
			volumes = append(volumes, Object{"name": "registry", "secret": Object{"secretName": "registry"}})
			mounts = append(mounts, Object{"name": "registry", "mountPath": "/registry", "readOnly": true})
		}
		job["volumeMounts"] = mounts
	}
	helper := Object{"name": "artifacts", "image": r.Image, "command": []string{"/service", "hold"}, "volumeMounts": []Object{{"name": "workspace", "mountPath": "/workspace", "readOnly": true}}, "securityContext": Security(), "resources": small}
	pod := Resource("v1", "Pod", "job", ns, Object{"metadata": Object{"name": "job", "namespace": ns, "labels": Object{"ci-preview/attempt": l.Attempt.ID}}, "spec": Object{"serviceAccountName": "workload", "automountServiceAccountToken": false, "enableServiceLinks": false, "restartPolicy": "Never", "activeDeadlineSeconds": timeout, "terminationGracePeriodSeconds": 5, "securityContext": Object{"runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "seccompProfile": Object{"type": "RuntimeDefault"}}, "volumes": volumes, "initContainers": []Object{init}, "containers": []Object{job, helper}}})
	return pod
}

type Result struct {
	Success                 bool
	Message, Digest         string
	Logs, Archive, Metadata []byte
}

func (r Runner) Execute(ctx context.Context, l domain.AttemptLease, token string) (Result, error) {
	var result Result
	if e := r.Prepare(ctx, l, token); e != nil {
		return result, e
	}
	ns := JobNamespace(l.Attempt.ID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
			v, e := r.Client.Get(ctx, "pod", ns, "job")
			if e != nil {
				return result, e
			}
			if v == nil {
				return result, errors.New("execution pod disappeared")
			}
			raw, _ := json.Marshal(v)
			var pod struct {
				Status struct {
					Phase             string `json:"phase"`
					Reason            string `json:"reason"`
					ContainerStatuses []struct {
						Name  string `json:"name"`
						State struct {
							Terminated *struct {
								ExitCode int    `json:"exitCode"`
								Reason   string `json:"reason"`
							} `json:"terminated"`
						} `json:"state"`
					} `json:"containerStatuses"`
				} `json:"status"`
			}
			if e = json.Unmarshal(raw, &pod); e != nil {
				return result, e
			}
			done := pod.Status.Phase == "Failed"
			result.Message = pod.Status.Reason
			for _, c := range pod.Status.ContainerStatuses {
				if c.Name == "job" && c.State.Terminated != nil {
					done = true
					result.Success = c.State.Terminated.ExitCode == 0
					result.Message = fmt.Sprintf("Container exited with code %d (%s)", c.State.Terminated.ExitCode, c.State.Terminated.Reason)
				}
			}
			if !done {
				continue
			}
			logs, e := r.Client.Run(ctx, nil, "logs", "-n", ns, "job", "-c", "job", "--limit-bytes=1048576")
			if e != nil {
				logs, _ = r.Client.Run(ctx, nil, "logs", "-n", ns, "job", "-c", "checkout", "--limit-bytes=1048576")
			}
			result.Logs = logs
			if result.Success {
				archive, e := r.Client.Run(ctx, nil, "exec", "-n", ns, "job", "-c", "artifacts", "--", "/service", "archive")
				if e != nil {
					return result, e
				}
				if len(archive) > 16<<20 {
					return result, errors.New("artifacts exceed 16 MiB")
				}
				result.Archive = archive
			}
			if result.Success && l.Job.Spec.Buildkit != nil {
				metadata, e := r.Client.Run(ctx, nil, "exec", "-n", ns, "job", "-c", "artifacts", "--", "/service", "metadata")
				if e != nil {
					return result, e
				}
				result.Metadata = metadata
				var data map[string]json.RawMessage
				if e = json.Unmarshal(metadata, &data); e != nil {
					return result, e
				}
				if e = json.Unmarshal(data["containerimage.digest"], &result.Digest); e != nil {
					return result, e
				}
			}
			return result, nil
		}
	}
}
