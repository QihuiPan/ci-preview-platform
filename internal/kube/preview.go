package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

type PreviewController struct {
	Client                          Client
	Domain, IngressClass, TLSSecret string
}

func (c PreviewController) Reconcile(ctx context.Context, p domain.Preview) (domain.State, string, error) {
	owner := p.Namespace
	if p.Desired == domain.StateDeleting {
		exists, e := c.Client.OwnedNamespace(ctx, p.Namespace, owner)
		if e != nil {
			return domain.StatePending, "", e
		}
		if !exists {
			return domain.StateDeleted, "", nil
		}
		e = c.Client.DeleteNamespace(ctx, p.Namespace, owner)
		return domain.StatePending, "", e
	}
	parts := strings.Split(p.Image, "@")
	if len(parts) != 2 || !planner.DigestPattern.MatchString(parts[1]) {
		return domain.StatePending, "", errors.New("preview requires a published immutable image digest")
	}
	if _, e := c.Client.OwnedNamespace(ctx, p.Namespace, owner); e != nil {
		return domain.StatePending, "", e
	}
	if e := c.Client.Apply(ctx, Namespace(p.Namespace, owner, "preview", true)); e != nil {
		return domain.StatePending, "", e
	}
	objects := Boundaries(p.Namespace, p.Port)
	for _, name := range []string{c.TLSSecret, os.Getenv("PREVIEW_PULL_SECRET")} {
		if name == "" {
			continue
		}
		secret, e := c.Client.Get(ctx, "secret", os.Getenv("PLATFORM_NAMESPACE"), name)
		if e != nil {
			return domain.StatePending, "", e
		}
		if secret == nil {
			return domain.StatePending, "", errors.New("configured preview secret does not exist")
		}
		objects = append(objects, Resource("v1", "Secret", name, p.Namespace, Object{"type": secret["type"], "data": secret["data"]}))
	}
	objects = append(objects, c.Manifests(p)...)
	if e := c.Client.Apply(ctx, objects...); e != nil {
		return domain.StatePending, "", e
	}
	deployment, e := c.Client.Get(ctx, "deployment", p.Namespace, "preview")
	if e != nil {
		return domain.StatePending, "", e
	}
	ready, e := deploymentReady(deployment)
	if e != nil {
		return domain.StatePending, "", e
	}
	if !ready {
		return domain.StatePending, "", nil
	}
	url := fmt.Sprintf("http://preview.%s.svc.cluster.local:%d", p.Namespace, p.Port)
	if p.Exposure == "public" {
		if c.Domain == "" {
			return domain.StatePending, "", errors.New("PREVIEW_DOMAIN is required for public exposure")
		}
		scheme := "http"
		if c.TLSSecret != "" {
			scheme = "https"
		}
		url = scheme + "://" + p.Namespace + "." + c.Domain
	}
	return domain.StateActive, url, nil
}

func deploymentReady(deployment Object) (bool, error) {
	raw, e := json.Marshal(deployment)
	if e != nil {
		return false, e
	}
	var status struct {
		Metadata struct {
			Generation int64 `json:"generation"`
		} `json:"metadata"`
		Status struct {
			ObservedGeneration int64 `json:"observedGeneration"`
			Replicas           int   `json:"replicas"`
			AvailableReplicas  int   `json:"availableReplicas"`
			UpdatedReplicas    int   `json:"updatedReplicas"`
		} `json:"status"`
	}
	if e = json.Unmarshal(raw, &status); e != nil {
		return false, e
	}
	// An old available replica must not mask an unready new image during rollout.
	s := status.Status
	return status.Metadata.Generation > 0 && s.ObservedGeneration >= status.Metadata.Generation && s.Replicas == 1 && s.UpdatedReplicas == 1 && s.AvailableReplicas == 1, nil
}
func (c PreviewController) Manifests(p domain.Preview) []Object {
	labels := Object{"app": "preview"}
	container := Object{"name": "app", "image": p.Image, "ports": []Object{{"containerPort": p.Port}}, "securityContext": Security(), "resources": Object{"requests": Object{"cpu": "100m", "memory": "128Mi", "ephemeral-storage": "64Mi"}, "limits": Object{"cpu": "1", "memory": "512Mi", "ephemeral-storage": "1Gi"}}, "readinessProbe": Object{"httpGet": Object{"path": p.HealthPath, "port": p.Port}, "initialDelaySeconds": 2, "periodSeconds": 3}, "livenessProbe": Object{"httpGet": Object{"path": p.HealthPath, "port": p.Port}, "initialDelaySeconds": 20, "periodSeconds": 10}, "volumeMounts": []Object{{"name": "tmp", "mountPath": "/tmp"}, {"name": "cache", "mountPath": "/var/cache/nginx"}, {"name": "run", "mountPath": "/var/run"}}}
	podSpec := Object{"serviceAccountName": "workload", "automountServiceAccountToken": false, "enableServiceLinks": false, "securityContext": Object{"runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "seccompProfile": Object{"type": "RuntimeDefault"}}, "containers": []Object{container}, "volumes": []Object{{"name": "tmp", "emptyDir": Object{"sizeLimit": "256Mi"}}, {"name": "cache", "emptyDir": Object{"sizeLimit": "256Mi"}}, {"name": "run", "emptyDir": Object{"sizeLimit": "64Mi"}}}}
	template := Object{"metadata": Object{"labels": labels, "annotations": Object{"ci-preview/pipeline": p.PipelineID}}, "spec": podSpec}
	if name := os.Getenv("PREVIEW_PULL_SECRET"); name != "" {
		podSpec["imagePullSecrets"] = []Object{{"name": name}}
	}
	deployment := Resource("apps/v1", "Deployment", "preview", p.Namespace, Object{"spec": Object{"replicas": 1, "revisionHistoryLimit": 2, "selector": Object{"matchLabels": labels}, "template": template}})
	objects := []Object{deployment, Resource("v1", "Service", "preview", p.Namespace, Object{"spec": Object{"selector": labels, "ports": []Object{{"port": p.Port, "targetPort": p.Port}}}})}
	if p.Exposure == "public" && c.Domain != "" {
		host := p.Namespace + "." + c.Domain
		spec := Object{"ingressClassName": c.IngressClass, "rules": []Object{{"host": host, "http": Object{"paths": []Object{{"path": "/", "pathType": "Prefix", "backend": Object{"service": Object{"name": "preview", "port": Object{"number": p.Port}}}}}}}}}
		if c.TLSSecret != "" {
			spec["tls"] = []Object{{"hosts": []string{host}, "secretName": c.TLSSecret}}
		}
		objects = append(objects, Resource("networking.k8s.io/v1", "Ingress", "preview", p.Namespace, Object{"spec": spec}))
	}
	return objects
}
