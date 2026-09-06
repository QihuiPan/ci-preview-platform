#!/usr/bin/env bash
set -euo pipefail
# Run only against a disposable cluster named ci-preview-test.
test "$(kubectl config current-context)" = "kind-ci-preview-test"
docker build -t ci-preview-platform:e2e .
docker build -f Dockerfile.minio -t ci-preview-minio:2025-10-15 .
kind load docker-image ci-preview-platform:e2e --name ci-preview-test
kind load docker-image ci-preview-minio:2025-10-15 --name ci-preview-test
kubectl create namespace ci-platform
kubectl label namespace ci-platform ci-preview/ingress=true
go run ./cmd/cictl init | kubectl apply -f -
helm lint deploy/helm/ci-preview-platform
helm upgrade --install ci deploy/helm/ci-preview-platform -n ci-platform --set image=ci-preview-platform:e2e --wait --wait-for-jobs --timeout 10m
docker pull nginxinc/nginx-unprivileged:1.27-alpine
export E2E_PREVIEW_IMAGE
E2E_PREVIEW_IMAGE=$(docker image inspect nginxinc/nginx-unprivileged:1.27-alpine --format '{{index .RepoDigests 0}}')
export E2E_API_TOKEN
E2E_API_TOKEN=$(kubectl get secret ci-secrets -n ci-platform -o jsonpath='{.data.admin-token}' | base64 --decode)
export E2E_API_URL=http://127.0.0.1:18080
(while true; do kubectl port-forward -n ci-platform service/ci-api 18080:8080; sleep 1; done) > /tmp/ci-port-forward.log 2>&1 &
forward_pid=$!
trap 'pkill -P "$forward_pid" 2>/dev/null || true; kill "$forward_pid" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do
  if curl -fsS "$E2E_API_URL/readyz" >/dev/null; then break; fi
  sleep 2
done
go test -v -count=1 -timeout 15m ./tests/e2e
