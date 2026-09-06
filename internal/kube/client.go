// Package kube applies operator-owned manifests through the pinned kubectl binary.
package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Object = map[string]any
type Client struct{ Binary string }
type bounded struct {
	bytes.Buffer
	limit int
}

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < b.limit {
		keep := b.limit - b.Len()
		if keep > len(p) {
			keep = len(p)
		}
		b.Buffer.Write(p[:keep])
	}
	return n, nil
}
func (c Client) Run(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	binary := c.Binary
	if binary == "" {
		binary = "kubectl"
	}
	args = append([]string{"--request-timeout=30s"}, args...)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = bytes.NewReader(input)
	out := &bounded{limit: 17 << 20}
	stderr := &bounded{limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if e := cmd.Run(); e != nil {
		return out.Bytes(), fmt.Errorf("kubectl failed: %w: %s", e, stderr.String())
	}
	return out.Bytes(), nil
}
func (c Client) Apply(ctx context.Context, objects ...Object) error {
	b, e := json.Marshal(Object{"apiVersion": "v1", "kind": "List", "items": objects})
	if e != nil {
		return e
	}
	_, e = c.Run(ctx, b, "apply", "--server-side", "--field-manager=ci-preview-platform", "-f", "-")
	return e
}
func (c Client) Get(ctx context.Context, kind, namespace, name string) (Object, error) {
	args := []string{"get", kind, name, "--ignore-not-found", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	b, e := c.Run(ctx, nil, args...)
	if e != nil {
		return nil, e
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	var out Object
	e = json.Unmarshal(b, &out)
	return out, e
}
func (c Client) OwnedNamespace(ctx context.Context, name, owner string) (bool, error) {
	v, e := c.Get(ctx, "namespace", "", name)
	if e != nil {
		return false, e
	}
	if v == nil {
		return false, nil
	}
	m, _ := v["metadata"].(map[string]any)
	labels, _ := m["labels"].(map[string]any)
	if labels["ci-preview/owner"] != owner {
		return true, errors.New("refusing to modify a namespace owned by another application")
	}
	return true, nil
}
func (c Client) DeleteNamespace(ctx context.Context, name, owner string) error {
	exists, e := c.OwnedNamespace(ctx, name, owner)
	if e != nil || !exists {
		return e
	}
	_, e = c.Run(ctx, nil, "delete", "namespace", name, "--wait=false", "--ignore-not-found")
	return e
}
func Resource(api, kind, name, namespace string, spec Object) Object {
	o := Object{"apiVersion": api, "kind": kind, "metadata": Object{"name": name}}
	if namespace != "" {
		o["metadata"].(Object)["namespace"] = namespace
	}
	for k, v := range spec {
		o[k] = v
	}
	return o
}
func JobNamespace(id string) string { return "ci-job-" + strings.ReplaceAll(id, "_", "-") }
func Namespace(name, owner, kind string, restricted bool) Object {
	level := "restricted"
	if !restricted {
		// Rootless BuildKit needs an explicit seccomp/AppArmor exception in trusted namespaces.
		level = "privileged"
	}
	return Resource("v1", "Namespace", name, "", Object{"metadata": Object{"name": name, "labels": Object{"ci-preview/owner": owner, "ci-preview/kind": kind, "pod-security.kubernetes.io/enforce": level, "pod-security.kubernetes.io/enforce-version": "latest"}}})
}
func Security() Object {
	return Object{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": Object{"drop": []string{"ALL"}}, "seccompProfile": Object{"type": "RuntimeDefault"}}
}

// Boundaries deny cross-namespace and metadata access while permitting DNS and public HTTPS.
func Boundaries(ns string, previewPort int) []Object {
	egress := []Object{{"to": []Object{{"namespaceSelector": Object{"matchLabels": Object{"kubernetes.io/metadata.name": "kube-system"}}}}, "ports": []Object{{"protocol": "UDP", "port": 53}, {"protocol": "TCP", "port": 53}}}, {"to": []Object{{"ipBlock": Object{"cidr": "0.0.0.0/0", "except": []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8", "100.64.0.0/10"}}}}, "ports": []Object{{"protocol": "TCP", "port": 443}}}}
	ingress := []Object{}
	if previewPort > 0 {
		ingress = append(ingress, Object{"from": []Object{{"namespaceSelector": Object{"matchLabels": Object{"ci-preview/ingress": "true"}}}}, "ports": []Object{{"protocol": "TCP", "port": previewPort}}})
	}
	return []Object{Resource("v1", "ServiceAccount", "workload", ns, Object{"automountServiceAccountToken": false}), Resource("v1", "ResourceQuota", "workload", ns, Object{"spec": Object{"hard": Object{"requests.cpu": "10", "requests.memory": "20Gi", "limits.cpu": "10", "limits.memory": "20Gi", "pods": "2", "services": "2", "secrets": "3", "requests.ephemeral-storage": "20Gi", "limits.ephemeral-storage": "20Gi"}}}), Resource("networking.k8s.io/v1", "NetworkPolicy", "isolation", ns, Object{"spec": Object{"podSelector": Object{}, "policyTypes": []string{"Ingress", "Egress"}, "ingress": ingress, "egress": egress}})}
}
