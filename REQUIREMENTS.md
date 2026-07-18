# Renovate-Trigger: Requirements

## Overview

A Go service that reacts to GitHub tag events and triggers Renovate runs on the
repositories that **consume** the newly-tagged code. When a **dependency** repo
publishes a tag, the service reads that repo's `renovate.trigger.json` to learn
its **dependents**, batches dependents across a short window, and creates a
one-off Kubernetes `Job` cloned from an existing Renovate `CronJob` template —
injecting the dependents via `RENOVATE_REPOSITORIES`.

A single **GitHub App** both delivers the webhooks and grants read access to the
trigger files. See `CONTEXT.md` for domain vocabulary and `docs/adr/` for the
decisions behind this shape.

## Functional Requirements

### FR-1: GitHub Webhook Ingestion
- Accept POST requests on `/webhook`, delivered by the GitHub App.
- Validate authenticity via `X-Hub-Signature-256` HMAC-SHA256 against the App's
  webhook secret (`RT_WEBHOOK_SECRET`).
- Reject invalid or missing signatures (HTTP 403).
- Enforce a 1MB request body size limit.

### FR-2: Event Filtering
- Process only `create` events (`X-GitHub-Event: create`).
- Within create events, process only tag creation (`ref_type: "tag"`).
- The tag name/version is irrelevant — any tag qualifies.
- Ignore all other event types with HTTP 200 (not an error for GitHub).

### FR-3: Opt-in by Trigger Declaration
- A repository opts in by keeping a `renovate.trigger.json` file at the root of
  its default branch. The filename is fixed (not configurable).
- Shape: `{"tags": ["org/dependent-a", "org/dependent-b"]}` — the list of
  dependent repositories to run Renovate on when this repository is tagged.
- There is **no** central allowlist. The opt-in gate is: the App is installed on
  the repo **and** the file is present. A tagged repo with no file is ignored.
- Presence/absence of the file is evaluated at flush time (FR-5), not on the
  request path.

### FR-4: Batch Collection
- On an accepted tag event, add the **source (dependency) repository** to an
  in-memory batch and return HTTP 202. No GitHub calls occur on the request path.
- The batch is a deduplicated set. The window is a **tumbling** window: the timer
  starts on the first repo added and fires once after the configured duration
  (default 30s); repos arriving during the window join the current batch, and the
  next repo after a flush starts a fresh window.

### FR-5: Flush & Resolution
- When the window fires, resolve the batch: for each unique source repository,
  discover its App installation, fetch `renovate.trigger.json` from its default
  branch, parse it, and collect its dependents.
- Take the deduplicated **union** of all dependents across the batch.
- Per-source failure degradation (each is non-fatal; one broken source never
  sinks the batch):
  - File missing (404) → treat as opt-out; log at DEBUG.
  - Fetch/installation error → drop that source; log at WARN.
  - Malformed JSON / missing `tags` key → drop that source; log at WARN.
  - Dependent entry that is not `owner/repo` → skip that entry; log at WARN.
- If the union is empty, create **no** Job; log at INFO.

### FR-6: Mutual Exclusion (no overlapping Renovate runs)
- Before creating a Renovate run, ensure no Renovate run is currently active.
- A run is **active** if a Job in the CronJob namespace is neither Complete nor
  Failed and is either (a) labelled `app.kubernetes.io/managed-by:
  renovate-trigger` (ours) or (b) owned by the source CronJob (a scheduled run).
- If an active run exists, **postpone**: do not drain the batch; re-arm the timer
  (reusing the batch window as the poll interval) and keep accumulating incoming
  tags. Repeat until no run is active, then flush. The wait is unbounded; emit a
  WARN once postponed beyond ~10 cycles for visibility.
- Exclusion is **best-effort** — a small TOCTOU window against the CronJob
  controller is accepted (the CronJob is not suspended).

### FR-7: Kubernetes Job Creation
- Read the source Renovate CronJob and deep-copy its `JobTemplate` (never mutate
  the cached object).
- Override `RENOVATE_REPOSITORIES` in every container with a JSON array of the
  resolved dependents (e.g. `'["org/app-a","org/app-b"]'`).
- Create a one-off Job with `GenerateName: "renovate-trigger-"`.
- Label with `app.kubernetes.io/managed-by: renovate-trigger`.
- Annotate with the source dependents and trigger timestamp.

### FR-8: Health Probes
- `GET /healthz` — liveness, always 200.
- `GET /readyz` — readiness, 200 if the Kubernetes API is reachable (cached 5s).
  Readiness does **not** depend on GitHub or on the CronJob.

## Non-Functional Requirements

### NFR-1: Configuration
- Operational config via environment variables; domain config (the dependency
  graph) is decentralized in per-repo `renovate.trigger.json` files.
- `RT_LISTEN_ADDR` — HTTP listen address (default `:8080`).
- `RT_LOG_LEVEL` — debug, info, warn, error (default `info`).
- `RT_WEBHOOK_SECRET` — GitHub App webhook secret (required).
- `RT_BATCH_WINDOW_SECONDS` — batch window / postpone poll interval (default 30).
- `RT_CRONJOB_NAME` — source CronJob name (required).
- `RT_CRONJOB_NAMESPACE` — source CronJob namespace (required).
- `RT_GITHUB_CLIENT_ID` — GitHub App client ID, used as the JWT `iss` (required).
- `RT_GITHUB_APP_PRIVATE_KEY_FILE` — path to the App private key PEM, mounted from
  a Secret (required).
- Startup validation (fail loud at boot): all required env present; log level
  valid; private key file readable and parses as RSA; the source CronJob is
  gettable. Any failure exits non-zero. No live GitHub token mint at boot.

### NFR-2: Security
- Webhook secret and App private key provided via environment / mounted Secret,
  never hardcoded.
- HMAC-SHA256 signature validation with constant-time comparison.
- GitHub App auth: client ID in a hand-rolled RS256 JWT; installation discovered
  per-repository; installation token cached and re-minted on expiry.
- GitHub App permission: `Contents: read` only.
- In-cluster K8s auth only (ServiceAccount).
- RBAC least privilege in the CronJob namespace: `get` cronjobs, `create` and
  `list` jobs.
- Container runs as non-root with read-only filesystem.
- Distroless base image (no shell, minimal attack surface).

### NFR-3: Observability
- Structured JSON logging via `log/slog`, configurable level.
- DEBUG: ignored events, opt-out (no trigger file). INFO: job created,
  startup/shutdown, batch flush, empty-union no-op. WARN: invalid signatures,
  per-source resolution failures, postponed-too-long. ERROR: K8s failures.
- No sensitive data in logs (no secrets, no private key, no raw signatures).

### NFR-4: Reliability
- Single replica is an enforced invariant: `replicas: 1`, `strategy: Recreate`,
  no HorizontalPodAutoscaler. See ADR-0001.
- Lossy by design: pending batches are not persisted and triggers are not
  retried — dropped on shutdown, crash, or failed flush. The source CronJob
  schedule is the backstop. See ADR-0002.
- Graceful shutdown on SIGINT/SIGTERM. HTTP timeouts: read 10s, write 10s,
  idle 60s.
- Thread-safe batch collector using `sync.Mutex`.

### NFR-5: Deployment
- Helm chart with: Deployment (`replicas: 1`, `Recreate`), Service,
  ServiceAccount, Role, RoleBinding, and Secrets for the webhook secret and the
  App private key.
- Multi-stage Docker build: `golang:1.24-alpine` builder,
  `distroless/static-debian12:nonroot` runtime.
- Multi-arch image (`linux/amd64`, `linux/arm64` via `docker buildx`): the
  builder stage runs on the native `$BUILDPLATFORM` (no QEMU emulation) and
  cross-compiles to `$TARGETOS`/`$TARGETARCH`.
- Static binary: `CGO_ENABLED=0`, stripped with `-ldflags="-s -w"`.
- Resource defaults: 50m/64Mi requests, 200m/128Mi limits.

### NFR-6: Testing
- 80%+ test coverage on internal packages. No test hits the network or sleeps on
  a real timer; all external I/O (GitHub, Kubernetes) is faked.
- **Seams:** the collector orchestrates three injected interfaces — `RunGate`
  (mutual-exclusion check over K8s), `Resolver` (source repos → deduped
  dependents over GitHub), and `JobCreator` — each faked in isolation.
- **Timing:** the gated-drain decision is a directly-callable method
  (`attemptFlush`) tested by scripting the `RunGate` (`active → active → clear`);
  the `time.AfterFunc` trigger stays a trivial, untested wire.
- **Unit tests for:** config validation (incl. private key parse), signature
  validation, event filtering + adding the source repo, batch collection &
  tumbling/postpone logic, `RunGate` detection (client-go fake with Jobs in
  various states/owners), `Resolver` logic via a `githubClient` interface fake
  (union/dedup, 404 opt-out, error/malformed drop, bad-entry skip, empty union),
  the GitHub client impl via `httptest` + a generated RSA key (JWT `iss`/RS256/exp,
  installation discovery, token cache/expiry, 404 vs 5xx), JWT signing, and job
  creation + env override.
- **Integration test:** real handler → real collector → fake `RunGate` + fake
  `Resolver` + client-go fake, asserting one Job with the expected deduped
  dependents from a signed webhook.

## Configuration Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `RT_LISTEN_ADDR` | No | `:8080` | HTTP listen address |
| `RT_LOG_LEVEL` | No | `info` | debug, info, warn, error |
| `RT_WEBHOOK_SECRET` | Yes | — | GitHub App webhook secret |
| `RT_BATCH_WINDOW_SECONDS` | No | `30` | Batch window / postpone poll interval (seconds) |
| `RT_CRONJOB_NAME` | Yes | — | Source CronJob name |
| `RT_CRONJOB_NAMESPACE` | Yes | — | Source CronJob namespace |
| `RT_GITHUB_CLIENT_ID` | Yes | — | GitHub App client ID (JWT issuer) |
| `RT_GITHUB_APP_PRIVATE_KEY_FILE` | Yes | — | Path to the App private key PEM |

## Dependencies

| Dependency | Purpose |
|---|---|
| `k8s.io/client-go` | Kubernetes API client |
| `k8s.io/api` | Kubernetes types (Job, CronJob) |
| `k8s.io/apimachinery` | Kubernetes meta types |
| `google/go-github` | GitHub REST client (contents, installations) |
| `golang-jwt/jwt` | Hand-rolled RS256 App JWT |
| `log/slog` (stdlib) | Structured JSON logging |
| `crypto/hmac` (stdlib) | Webhook signature validation |
