# Renovate-Trigger: Requirements

## Overview

A Go application that listens for GitHub webhooks, detects tag creation events on allowlisted repositories, and triggers Renovate runs by creating Kubernetes Jobs from an existing CronJob template.

## Functional Requirements

### FR-1: GitHub Webhook Ingestion
- Accept POST requests on `/webhook` endpoint
- Validate webhook authenticity via `X-Hub-Signature-256` HMAC-SHA256 signature
- Reject requests with invalid or missing signatures (HTTP 403)
- Enforce 1MB request body size limit

### FR-2: Event Filtering
- Process only `create` events (`X-GitHub-Event: create` header)
- Within create events, process only tag creation (`ref_type: "tag"`)
- Ignore all other event types with HTTP 200 (not an error for GitHub)

### FR-3: Repository Allowlist
- Maintain a list of allowed repositories in `renovate-trigger.yaml` config file
- Match `repository.full_name` from webhook payload against the allowlist
- Ignore events from repositories not in the allowlist

### FR-4: Batch Collection
- Collect matching tag events within a configurable time window (default: 30 seconds)
- Deduplicate repositories within a batch window
- Create a single Renovate Job per batch (not per event)
- Timer starts on first event in a window; fires after the configured duration

### FR-5: Kubernetes Job Creation
- Read the existing Renovate CronJob spec as a template
- Deep copy the CronJob's `JobTemplate` (never mutate the cached object)
- Override `RENOVATE_REPOSITORIES` env var with a JSON array of batched repos (e.g. `'["org/repo-a","org/repo-b"]'`)
- Create a one-off Job via the Kubernetes API
- Use `GenerateName: "renovate-trigger-"` for unique job naming
- Label jobs with `app.kubernetes.io/managed-by: renovate-trigger`
- Annotate jobs with source repos and trigger timestamp

### FR-6: Health Probes
- `GET /healthz` — liveness probe, always returns 200
- `GET /readyz` — readiness probe, returns 200 if K8s API is reachable (cached for 5 seconds)

## Non-Functional Requirements

### NFR-1: Configuration
- YAML config file (`renovate-trigger.yaml`) as base configuration
- Environment variable overrides via `RT_` prefix (e.g. `RT_LISTEN_ADDR`, `RT_LOG_LEVEL`)
- Powered by Viper library for config management
- Config fields: `listenAddr`, `logLevel`, `webhookSecret`, `batchWindowSeconds`, `cronjob.name`, `cronjob.namespace`, `repos`
- Startup validation: all required fields must be present and valid

### NFR-2: Security
- Webhook secret provided via environment variable (`RT_WEBHOOKSECRET`), never hardcoded
- HMAC-SHA256 signature validation with constant-time comparison
- In-cluster K8s auth only (ServiceAccount)
- RBAC least privilege: only `get` CronJobs and `create` Jobs in the target namespace
- Container runs as non-root with read-only filesystem
- Distroless base image (no shell, minimal attack surface)

### NFR-3: Observability
- Structured JSON logging via `log/slog`
- Configurable log level: debug, info, warn, error
- Log levels: DEBUG (ignored events), INFO (job created, startup/shutdown, batch flush), WARN (invalid signatures), ERROR (K8s failures)
- No sensitive data in logs (no secrets, no raw signatures)

### NFR-4: Reliability
- Graceful shutdown on SIGINT/SIGTERM
- HTTP server timeouts: read 10s, write 10s, idle 60s
- Failed batch flushes are logged and discarded (no retry)
- Thread-safe batch collector using `sync.Mutex`

### NFR-5: Deployment
- Helm chart with: Deployment, Service, ServiceAccount, Role, RoleBinding, ConfigMap, Secret
- Multi-stage Docker build: `golang:1.24-alpine` builder, `distroless/static-debian12:nonroot` runtime
- Static binary: `CGO_ENABLED=0`, stripped with `-ldflags="-s -w"`
- Resource defaults: 50m/64Mi requests, 200m/128Mi limits

### NFR-6: Testing
- 80%+ test coverage on internal packages
- Unit tests for: config validation, signature validation, event filtering, batch collection, job creation, env override
- Integration tests using `client-go/kubernetes/fake` and `httptest`
- Test fixtures in `internal/webhook/testdata/`

## Configuration Reference

```yaml
listenAddr: ":8080"              # HTTP listen address
logLevel: "info"                 # debug | info | warn | error
webhookSecret: ""                # GitHub webhook secret (prefer RT_WEBHOOKSECRET env)
batchWindowSeconds: 30           # Batch collection window
cronjob:
  name: "renovate"               # Source CronJob name
  namespace: "renovate"           # Source CronJob namespace
repos:                           # Allowlisted repositories
  - "org/repo-a"
  - "org/repo-b"
```

All fields can be overridden via `RT_` prefixed environment variables.

## Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/spf13/viper` | Config: YAML + env var override |
| `k8s.io/client-go` | Kubernetes API client |
| `k8s.io/api` | Kubernetes types (Job, CronJob) |
| `k8s.io/apimachinery` | Kubernetes meta types |
| `log/slog` (stdlib) | Structured JSON logging |
| `crypto/hmac` (stdlib) | Webhook signature validation |
