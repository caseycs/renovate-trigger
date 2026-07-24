#!/usr/bin/env bash
#
# End-to-end test: install the Helm chart into a real (kind) cluster and prove
# the service actually boots and serves traffic — the whole path that
# `helm lint`, `helm template`, and helm-unittest cannot reach:
#
#   * the securityContext (runAsNonRoot + numeric UID 65532) lets the pod start;
#   * the in-cluster ServiceAccount + Role can `get` the source CronJob, so the
#     startup Verify() passes instead of crash-looping;
#   * the Secret wiring (client id, RSA private key PEM, webhook secret) parses;
#   * the readiness probe (/readyz) can reach the API server;
#   * the HTTP server accepts a correctly-signed /webhook (HTTP 202).
#
# Runnable locally (creates and tears down its own kind cluster) and in CI
# (point it at an existing cluster with CREATE_CLUSTER=0). All knobs are env
# vars; see the defaults below.
#
set -euo pipefail

CLUSTER="${CLUSTER:-renovate-trigger-e2e}"
NAMESPACE="${NAMESPACE:-renovate}"
RELEASE="${RELEASE:-renovate-trigger}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/caseycs/renovate-trigger}"
IMAGE_TAG="${IMAGE_TAG:-e2e}"
CRONJOB_NAME="${CRONJOB_NAME:-renovate}"

# Toggles — set to 0 in CI where the cluster/image are managed by the workflow.
CREATE_CLUSTER="${CREATE_CLUSTER:-1}"  # create + delete a kind cluster
BUILD_IMAGE="${BUILD_IMAGE:-1}"        # docker build the app image
LOAD_IMAGE="${LOAD_IMAGE:-1}"          # kind load the image into the cluster
KEEP="${KEEP:-0}"                      # keep cluster/resources on exit (debug)

PF_PORT="${PF_PORT:-18080}"

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

workdir=$(mktemp -d)
pf_pid=""

step()  { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
fail()  { printf '\n\033[1;31mFAIL:\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() {
  local code=$?
  [[ -n "$pf_pid" ]] && kill "$pf_pid" 2>/dev/null || true
  if [[ $code -ne 0 ]]; then
    step "Failure diagnostics"
    kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null || true
    kubectl describe pods -n "$NAMESPACE" -l "app.kubernetes.io/name=renovate-trigger" 2>/dev/null || true
    kubectl logs -n "$NAMESPACE" -l "app.kubernetes.io/name=renovate-trigger" --tail=50 2>/dev/null || true
  fi
  if [[ "$KEEP" == "1" ]]; then
    info "KEEP=1 — leaving cluster '$CLUSTER' and namespace '$NAMESPACE' in place"
  elif [[ "$CREATE_CLUSTER" == "1" ]]; then
    step "Deleting kind cluster '$CLUSTER'"
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  else
    kubectl delete namespace "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  fi
  rm -rf "$workdir"
  exit $code
}
trap cleanup EXIT

for bin in kubectl helm openssl curl; do
  command -v "$bin" >/dev/null || fail "required tool '$bin' not found in PATH"
done
[[ "$CREATE_CLUSTER" == "1" || "$LOAD_IMAGE" == "1" ]] && { command -v kind >/dev/null || fail "'kind' not found in PATH"; }
[[ "$BUILD_IMAGE" == "1" ]] && { command -v docker >/dev/null || fail "'docker' not found in PATH"; }

if [[ "$BUILD_IMAGE" == "1" ]]; then
  step "Building image ${IMAGE_REPO}:${IMAGE_TAG}"
  docker build -t "${IMAGE_REPO}:${IMAGE_TAG}" .
fi

if [[ "$CREATE_CLUSTER" == "1" ]]; then
  step "Creating kind cluster '$CLUSTER'"
  if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    info "cluster already exists — reusing"
  else
    kind create cluster --name "$CLUSTER" --wait 120s
  fi
fi

if [[ "$LOAD_IMAGE" == "1" ]]; then
  step "Loading image into kind cluster '$CLUSTER'"
  kind load docker-image "${IMAGE_REPO}:${IMAGE_TAG}" --name "$CLUSTER"
fi

step "Creating namespace '$NAMESPACE'"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

step "Creating source CronJob '$CRONJOB_NAME' (the app verifies it at startup)"
# Suspended so it never actually runs; the app only needs to GET it.
kubectl apply -n "$NAMESPACE" -f - <<EOF
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ${CRONJOB_NAME}
spec:
  schedule: "0 0 * * *"
  suspend: true
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: renovate
              image: renovate/renovate:latest
              command: ["renovate"]
EOF

step "Creating Secret with a throwaway RSA key + webhook secret"
WEBHOOK_SECRET="e2e-webhook-secret"
openssl genrsa -out "$workdir/github-app.pem" 2048 2>/dev/null
kubectl create secret generic renovate-trigger -n "$NAMESPACE" \
  --from-literal=github-client-id="Iv1.e2e000000000000" \
  --from-file=github-app-private-key="$workdir/github-app.pem" \
  --from-literal=webhook-secret="$WEBHOOK_SECRET" \
  --dry-run=client -o yaml | kubectl apply -f -

step "helm upgrade --install (waits for readiness)"
# --wait blocks until the readiness probe passes, so a green install already
# proves startup + liveness + readiness. The explicit checks below add coverage
# for the actual HTTP responses and the signed-webhook path.
helm upgrade --install "$RELEASE" chart \
  --namespace "$NAMESPACE" \
  --set image.repository="$IMAGE_REPO" \
  --set image.tag="$IMAGE_TAG" \
  --set image.pullPolicy=IfNotPresent \
  --set config.cronjob.name="$CRONJOB_NAME" \
  --set config.cronjob.namespace="$NAMESPACE" \
  --set config.batchWindowSeconds=5 \
  --wait --timeout 120s

step "Waiting for deployment rollout"
kubectl rollout status deployment/"$RELEASE" -n "$NAMESPACE" --timeout=120s

step "Port-forwarding svc/$RELEASE :8080 -> localhost:$PF_PORT"
kubectl port-forward -n "$NAMESPACE" "svc/$RELEASE" "${PF_PORT}:8080" >/dev/null 2>&1 &
pf_pid=$!
for _ in $(seq 1 20); do
  curl -fsS "localhost:${PF_PORT}/healthz" >/dev/null 2>&1 && break
  sleep 0.5
done

step "Asserting /healthz returns 200"
curl -fsS "localhost:${PF_PORT}/healthz" >/dev/null || fail "/healthz did not return 200"
info "ok"

step "Asserting /readyz returns 200 (probe reached the API server)"
curl -fsS "localhost:${PF_PORT}/readyz" >/dev/null || fail "/readyz did not return 200"
info "ok"

step "Asserting a correctly-signed /webhook tag event returns 202"
cat > "$workdir/payload.json" <<'JSON'
{"ref":"v1.0.0","ref_type":"tag","repository":{"full_name":"e2e-org/e2e-repo"}}
JSON
sig=$(openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" "$workdir/payload.json" | awk '{print $NF}')
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "localhost:${PF_PORT}/webhook" \
  -H "X-GitHub-Event: create" \
  -H "X-Hub-Signature-256: sha256=${sig}" \
  -H "Content-Type: application/json" \
  --data-binary @"$workdir/payload.json")
[[ "$code" == "202" ]] || fail "/webhook returned $code, expected 202"
info "ok (202 Accepted)"

step "Asserting an unsigned /webhook is rejected with 403"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "localhost:${PF_PORT}/webhook" \
  -H "X-GitHub-Event: create" \
  -H "Content-Type: application/json" \
  --data-binary @"$workdir/payload.json")
[[ "$code" == "403" ]] || fail "unsigned /webhook returned $code, expected 403"
info "ok (403 Forbidden)"

printf '\n\033[1;32mPASS\033[0m — chart installs and the service runs.\n'
