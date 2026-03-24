# Renovate-Trigger: Specification & Implementation Plan

## Context

When a new tag is pushed to certain GitHub repositories, we want to immediately trigger a Renovate run for that repo (rather than waiting for the next CronJob schedule). This app bridges GitHub webhooks to Kubernetes, creating one-off Jobs from an existing Renovate CronJob template with the target repo injected via `RENOVATE_REPOSITORIES` env var.

---

## Architecture

```
GitHub Webhook (POST /webhook)
        │
        ▼
┌──────────────────┐
│  HTTP Server     │
│  /webhook        │
│  /healthz        │
│  /readyz         │
└──────────────────┘
        │
        ▼
┌──────────────────┐
│  Webhook Handler │
│  - Sig validation│
│  - Event filter  │
│  - Repo matching │
└──────────────────┘
        │
        ▼
┌──────────────────┐
│  Batch Collector │
│  - Collects repos│
│  - Timer-based   │
│  - Flush on timer│
└──────────────────┘
        │
        ▼
┌──────────────────┐
│  K8s Job Creator │
│  - Get CronJob   │
│  - DeepCopy spec │
│  - Override env   │
│  - Create Job    │
└──────────────────┘
        │
        ▼
   Kubernetes API
```

### Request Flow (Happy Path)

1. GitHub POSTs to `/webhook` with `X-Hub-Signature-256` and `X-GitHub-Event: create`
2. Validate HMAC-SHA256 signature (constant-time compare)
3. If event != `create` → 200 ignored
4. Parse JSON; if `ref_type` != `"tag"` → 200 ignored
5. Check `repository.full_name` against allowlist → if not found, 200 ignored
6. Add repo to batch collector → respond 202 accepted
7. When batch window expires (default 30s), collector flushes:
   - Read CronJob spec, deep copy `JobTemplate`
   - Override `RENOVATE_REPOSITORIES` env to `'["org/repo-a","org/repo-b"]'` (all repos in batch)
   - Create single Job via K8s API

### Batching Behavior

- Webhook handler adds matched repos to an in-memory set (deduped)
- Fixed window timer: starts on first event, fires after configured duration (default 30s)
- On timer expiry: flush all collected repos into one Job, clear the set
- If batch is empty at flush time, no-op
- Thread-safe: use `sync.Mutex` to protect the repo set

### Error Handling

| Scenario | Status | Action |
|---|---|---|
| Invalid/missing signature | 403 | Log warn, reject |
| Malformed JSON | 400 | Log warn, reject |
| Non-create event / non-tag | 200 | Debug log, ignore |
| Repo not in allowlist | 200 | Info log, ignore |
| Repo accepted into batch | 202 | Info log, accepted |
| CronJob not found (at flush) | — | Log error, discard batch |
| Job creation failed (at flush) | — | Log error, discard batch |
| Bad config on startup | Fatal | Exit non-zero |

---

## Configuration

### Library: **Viper** (`github.com/spf13/viper`)

- YAML config file as base
- Environment variables override any YAML value
- Env prefix: `RT_` (e.g. `RT_LISTEN_ADDR`, `RT_CRONJOB_NAME`)
- Nested keys via `_` separator: `RT_CRONJOB_NAME`, `RT_CRONJOB_NAMESPACE`

### Config File: `renovate-trigger.yaml`

```yaml
listenAddr: ":8080"                    # default ":8080", env: RT_LISTEN_ADDR
logLevel: "info"                       # debug|info|warn|error, default "info", env: RT_LOG_LEVEL
webhookSecret: ""                      # env: RT_WEBHOOK_SECRET (preferred via env, not file)
batchWindowSeconds: 30                 # default 30, env: RT_BATCH_WINDOW_SECONDS
cronjob:
  name: "renovate"                     # required, env: RT_CRONJOB_NAME
  namespace: "renovate"                # required, env: RT_CRONJOB_NAMESPACE
repos:
  - "org/repo-a"
  - "org/repo-b"
```

### Validation at startup
- `cronjob.name` and `cronjob.namespace` must be non-empty
- `repos` must contain at least one entry
- `webhookSecret` must be non-empty (typically set via `RT_WEBHOOK_SECRET` env var)
- `batchWindowSeconds` must be > 0
- `logLevel` must be one of: debug, info, warn, error

---

## Project Structure

```
renovate-trigger/
├── cmd/renovate-trigger/
│   └── main.go                    # Entry point: config, k8s client, server, graceful shutdown
├── internal/
│   ├── config/
│   │   ├── config.go              # Config struct, Load() via Viper, Validate()
│   │   └── config_test.go
│   ├── webhook/
│   │   ├── handler.go             # HTTP handler, event filtering, repo matching
│   │   ├── handler_test.go
│   │   ├── signature.go           # HMAC-SHA256 validation
│   │   ├── signature_test.go
│   │   ├── payload.go             # GitHub event structs
│   │   └── testdata/              # Sample GitHub payloads for tests
│   ├── batch/
│   │   ├── collector.go           # Batch collector: repo set, timer, flush logic
│   │   └── collector_test.go
│   ├── k8s/
│   │   ├── client.go              # In-cluster clientset factory
│   │   ├── job.go                 # Read CronJob, build Job, override env, create
│   │   └── job_test.go
│   └── server/
│       ├── server.go              # Routes, health probes, graceful shutdown
│       └── server_test.go
├── chart/
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
│       ├── _helpers.tpl
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── serviceaccount.yaml
│       ├── role.yaml
│       ├── rolebinding.yaml
│       ├── configmap.yaml
│       └── secret.yaml
├── Dockerfile
├── go.mod
├── renovate-trigger.yaml.example
└── .gitignore
```

---

## Key Implementation Details

### Webhook Signature Validation (`internal/webhook/signature.go`)
- Read entire body into `[]byte` first (needed for both HMAC and JSON parsing)
- `hmac.New(sha256.New, secret)` → compare with `hmac.Equal`
- Strip `sha256=` prefix from header
- Enforce `http.MaxBytesReader` (1MB limit)

### GitHub Payload Structs (`internal/webhook/payload.go`)
```go
type CreateEvent struct {
    Ref        string     `json:"ref"`
    RefType    string     `json:"ref_type"`
    Repository Repository `json:"repository"`
}
type Repository struct {
    FullName string `json:"full_name"`
}
```

### JobCreator Interface (`internal/webhook/handler.go`)
```go
type JobCreator interface {
    CreateJobForRepos(ctx context.Context, repos []string) (jobName string, err error)
}
```
Decouples webhook handler from K8s — enables unit testing with mocks.

### Batch Collector (`internal/batch/collector.go`)
```go
type Collector struct {
    mu       sync.Mutex
    repos    map[string]struct{}   // deduped repo set
    timer    *time.Timer
    window   time.Duration
    onFlush  func(repos []string)  // callback to create Job
}

func (c *Collector) Add(repo string)   // add repo, start timer if first
func (c *Collector) flush()            // called by timer, sends repos to onFlush, clears set
func (c *Collector) Stop()             // cancel timer on shutdown
```

### K8s Job Creation (`internal/k8s/job.go`)
1. `clientset.BatchV1().CronJobs(ns).Get(ctx, name, ...)`
2. `cronJob.Spec.JobTemplate.Spec.DeepCopy()` — never mutate cached object
3. Override/append `RENOVATE_REPOSITORIES` env var on all containers: `json.Marshal(repos)` → `'["org/repo-a","org/repo-b"]'`
4. `GenerateName: "renovate-trigger-"` (K8s appends random suffix)
5. Labels: `app.kubernetes.io/managed-by: renovate-trigger`
6. Annotations: `renovate-trigger/repos` (comma-separated list), `renovate-trigger/triggered-at` (timestamp)

### Config Loading (`internal/config/config.go`)
```go
func Load() (Config, error) {
    v := viper.New()
    v.SetConfigName("renovate-trigger")
    v.SetConfigType("yaml")
    v.AddConfigPath("/etc/renovate-trigger")
    v.AddConfigPath(".")
    v.SetEnvPrefix("RT")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    v.AutomaticEnv()
    // Set defaults
    v.SetDefault("listenAddr", ":8080")
    v.SetDefault("logLevel", "info")
    v.SetDefault("batchWindowSeconds", 30)
    // Read config file (optional — env vars can provide everything)
    v.ReadInConfig()
    var cfg Config
    v.Unmarshal(&cfg)
    return cfg, cfg.Validate()
}
```

### Logging
- `log/slog` with JSON output
- Level configured via `logLevel` config / `RT_LOG_LEVEL` env var
- DEBUG: ignored events; INFO: job created, startup/shutdown, batch flush; WARN: invalid signatures; ERROR: K8s failures

### Health Probes
- `GET /healthz` → 200 always (liveness)
- `GET /readyz` → 200 if K8s API reachable (readiness), cached for a few seconds

---

## Helm Chart

### RBAC (least privilege, scoped to CronJob namespace)
```yaml
rules:
  - apiGroups: ["batch"]
    resources: ["cronjobs"]
    verbs: ["get"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create"]
```

### Deployment highlights
- ConfigMap mount at `/etc/renovate-trigger/renovate-trigger.yaml`
- Secret → `RT_WEBHOOK_SECRET` env var
- Security context: `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`
- Liveness: `/healthz`, Readiness: `/readyz`

### values.yaml key fields
```yaml
replicaCount: 1
image:
  repository: ghcr.io/org/renovate-trigger
  tag: ""
  pullPolicy: IfNotPresent
config:
  listenAddr: ":8080"
  logLevel: "info"
  batchWindowSeconds: 30
  cronjob:
    name: "renovate"
    namespace: "renovate"
  repos:
    - "org/repo-a"
    - "org/repo-b"
webhookSecret: ""  # set via --set or external secret
rbac:
  jobNamespace: "renovate"
resources:
  requests: { cpu: 50m, memory: 64Mi }
  limits: { cpu: 200m, memory: 128Mi }
```

### Dockerfile
- Multi-stage: `golang:1.24-alpine` builder → `gcr.io/distroless/static-debian12:nonroot` runtime
- `CGO_ENABLED=0`, `-ldflags="-s -w"` for static minimal binary

---

## Dependencies

- `github.com/spf13/viper` — config (YAML + env var override)
- `k8s.io/client-go` — K8s API client
- `k8s.io/api` — K8s types (Job, CronJob)
- `k8s.io/apimachinery` — K8s meta types
- Standard library: `net/http`, `crypto/hmac`, `crypto/sha256`, `encoding/json`, `log/slog`, `sync`, `time`

---

## Testing Strategy (80%+ coverage target)

### Unit Tests
| Package | Coverage Focus |
|---|---|
| `config` | Viper loading, env var override, defaults, validation errors |
| `webhook/signature` | Valid/invalid/missing signatures, empty body |
| `webhook/handler` | All event filtering paths, repo matching, batch collector called, HTTP status codes |
| `batch/collector` | Add/dedup repos, timer fires after window, flush callback invoked with correct repos, concurrent access |
| `k8s/job` | Job building from CronJob spec, env override (replace existing, append new), deep copy verification |

### Integration Tests
- `client-go/kubernetes/fake` clientset for full flow: config → webhook POST → batch flush → Job creation verification
- `httptest.NewServer` for HTTP layer

### E2E (manual/CI)
- Deploy to test cluster via Helm
- `curl` with sample webhook payload
- `kubectl get jobs` to verify

---

## Verification Plan

1. `go test ./...` — all unit + integration tests pass with 80%+ coverage
2. `go build ./cmd/renovate-trigger` — binary builds
3. `docker build .` — image builds
4. `helm template chart/` — Helm renders without errors
5. `helm lint chart/` — Helm lint passes
6. Deploy to test cluster, send curl webhook, wait for batch window, verify Job is created with correct `RENOVATE_REPOSITORIES` env

---

## Implementation Order

1. **Phase 1**: `internal/config` — Config struct, Viper loading, validation
2. **Phase 2**: `internal/webhook` — Signature validation, payload parsing, handler
3. **Phase 3**: `internal/batch` — Batch collector with timer
4. **Phase 4**: `internal/k8s` — Job creation from CronJob template
5. **Phase 5**: `internal/server` + `cmd/renovate-trigger/main.go` — Wire everything, graceful shutdown
6. **Phase 6**: `Dockerfile` + `.gitignore`
7. **Phase 7**: Helm chart
8. **Phase 8**: Tests for all packages
