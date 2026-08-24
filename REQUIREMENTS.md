# Renovate-Trigger: Requirements

## Overview

A Go service that reacts to publication events and triggers Renovate runs on the
repositories that **consume** what was published. When a **dependency** publishes
— a GitHub repository tags a commit, or an ECR repository receives an image push —
the service reads that dependency's trigger declaration to learn its
**dependents**, batches dependents across a short window, and creates a one-off
Kubernetes `Job` cloned from an existing Renovate `CronJob` template — injecting
the dependents via `RENOVATE_REPOSITORIES`.

ECR ingestion is optional and off unless a queue is configured; a GitHub-only
deployment behaves exactly as before.

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
- GitHub delivers one `create` per tag; several tags on the same repository
  within a window collapse into one batch entry (FR-4) and one run.
- **Platform limit (accepted):** GitHub delivers no `create` — and no `push` —
  event for tags when a single push creates more than three of them. Those tags
  are invisible to the service and cannot be recovered; the scheduled CronJob is
  the backstop (NFR-4). See [README → Limitations](./README.md#limitations).

### FR-3: Opt-in by Trigger Declaration (GitHub)
- A repository opts in by keeping a `renovate.trigger.json` file at the root of
  its default branch. The filename is fixed (not configurable).
- Shape: `{"tags": ["org/dependent-a", "org/dependent-b"]}` — the list of
  dependent repositories to run Renovate on when this repository is tagged.
- There is **no** central allowlist. The opt-in gate is: the App is installed on
  the repo **and** the file is present. A tagged repo with no file is ignored.
- Presence/absence of the file is evaluated at flush time (FR-5), not on the
  request path.

### FR-4: Batch Collection
- On an accepted publication event, add the **dependency** to an in-memory batch
  and return HTTP 202 (webhook) or acknowledge the queue message (ECR). No GitHub
  or AWS resolution calls occur on the ingestion path.
- A batch element is a `(kind, identifier)` pair — `("github", "owner/name")` or
  `("ecr", "<repository ARN>")` — and the batch is a deduplicated set over that
  pair. One multi-architecture image push therefore collapses to one entry.
- The window is a **tumbling** window: the timer starts on the first dependency
  added and fires once after the configured duration (default 30s); dependencies
  arriving during the window join the current batch, and the next one after a
  flush starts a fresh window.

### FR-5: Flush & Resolution
- When the window fires, resolve the batch. Resolution dispatches on the
  dependency kind: GitHub dependencies are resolved by discovering the App
  installation and fetching `renovate.trigger.json` from the default branch; ECR
  dependencies are resolved by reading the dependents tag off the repository via
  `ecr:ListTagsForResource`.
- Take the deduplicated **union** of all dependents across the batch.
- Per-source failure degradation (each is non-fatal; one broken source never
  sinks the batch):
  - File missing (404) → treat as opt-out; log at DEBUG.
  - Fetch/installation error → drop that source; log at WARN.
  - Malformed JSON / missing `tags` key → drop that source; log at WARN.
  - Dependent entry that is not `owner/repo` → skip that entry; log at WARN.
  - ECR: dependents tag absent, or repository deleted since the push
    (`RepositoryNotFoundException`) → treat as opt-out; log at DEBUG.
  - ECR: any other AWS error, notably `AccessDenied` → drop that dependency; log
    at WARN. A denied read must never look like a global opt-out.
  - A dependency whose kind has no registered fetcher (an ECR dependency with ECR
    disabled) → drop it; log at WARN.
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

### FR-8: ECR Image-Push Ingestion (optional)
- Enabled solely by `RT_ECR_QUEUE_URL` being set. Unset means no AWS client is
  constructed and nothing about the GitHub path changes.
- Long-poll the delivery queue: `WaitTimeSeconds: 20` (the SQS maximum),
  `MaxNumberOfMessages: 10`, `VisibilityTimeout: 30`.
- Accept an event only when `source` is `aws.ecr`, `detail-type` is
  `ECR Image Action`, `detail.action-type` is `PUSH`, and `detail.result` is
  `SUCCESS`. The same detail-type is reused for `DELETE` and
  `UPDATE_STORAGE_CLASS`, so the action must be checked.
- `detail.image-tag` is **not** required: it is absent for untagged (digest-only)
  pushes, and the concrete image tag is as irrelevant as a git tag name is.
- Identify the dependency by the repository ARN, constructed from the event's
  `account`, `region`, and `detail.repository-name` — `resources` is empty for this
  detail-type. Derive the ARN partition from the region so GovCloud, China, and
  isolated regions work.
- Add the dependency to the batch, then **delete the message immediately**.
  At-most-once matches NFR-4's lossy-by-design stance, and holding receipt handles
  across a window and unbounded postpones would outlive the visibility timeout.
- A well-formed event that is filtered out is deleted (DEBUG). A body that cannot
  be parsed is **not** deleted, so the redrive policy surfaces it in the
  dead-letter queue rather than the service hiding it (WARN).
- Receive failures retry with capped exponential backoff (1s doubling to 30s)
  indefinitely, and never terminate the process: a broken ECR path must not take
  GitHub ingestion down with it. WARN once at 10 consecutive failures.

### FR-9: Opt-in by Dependents Tag (ECR)
- An ECR repository opts in by carrying a resource tag whose key is
  `RT_ECR_DEPENDENTS_TAG` (default `renovate-trigger/dependents`), holding a
  comma-separated list of dependents: `"org/app-a,org/app-b"`.
- Whitespace around entries is trimmed and empty entries dropped; whitespace also
  works as a separator, because AWS's cross-service guidance on allowed tag-value
  characters does not list the comma.
- Presence of the tag is the opt-in; absence is a normal opt-out. An empty value is
  a declaration listing nothing.
- There is **no** central allowlist of ECR repositories. The EventBridge rule
  matches every image push in the account by default, and a repository without the
  tag is dropped after one cheap tag read.
- AWS caps a tag value at 256 characters, bounding an ECR repository to roughly a
  dozen dependents.

### FR-10: Health Probes
- `GET /healthz` — liveness, always 200.
- `GET /readyz` — readiness, 200 if the Kubernetes API is reachable (cached 5s).
  Readiness does **not** depend on GitHub, on AWS, or on the CronJob.

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
- `RT_ECR_QUEUE_URL` — SQS queue URL carrying ECR image-push events. Empty (the
  default) disables ECR ingestion entirely; it is the single enablement signal.
- `RT_ECR_DEPENDENTS_TAG` — AWS tag key holding an ECR repository's dependents
  (default `renovate-trigger/dependents`).
- `RT_AWS_REGION` — region for the AWS clients. Required in practice under EKS Pod
  Identity, which injects credentials but no region.
- Startup validation (fail loud at boot): all required env present; log level
  valid; private key file readable and parses as RSA; the source CronJob is
  gettable. When ECR is enabled, additionally: the queue URL is an `https://` URL,
  a region resolves, and the queue's attributes are gettable — a wrong URL or a
  missing IAM policy crash-loops the pod rather than leaving it deployed and
  silently ingesting nothing. Any failure exits non-zero. No live GitHub token mint
  at boot.

### NFR-2: Security
- Webhook secret and App private key provided via environment / mounted Secret,
  never hardcoded.
- HMAC-SHA256 signature validation with constant-time comparison.
- GitHub App auth: client ID in a hand-rolled RS256 JWT; installation discovered
  per-repository; installation token cached and re-minted on expiry.
- GitHub App permission: `Contents: read` only.
- AWS credentials via EKS Pod Identity only — never static keys. The trust policy
  grants `sts:AssumeRole` and `sts:TagSession` to `pods.eks.amazonaws.com`, and the
  association binds the role to one namespace and one ServiceAccount.
- `AWS_EC2_METADATA_DISABLED=true` whenever ECR is enabled, so a misconfigured
  association fails at the boot-time queue check instead of silently falling back
  to the node instance profile.
- AWS least privilege: `sqs:ReceiveMessage`, `sqs:DeleteMessage` and
  `sqs:GetQueueAttributes` on the one delivery queue; `ecr:ListTagsForResource`
  scoped to ECR repositories in the account and region; `kms:Decrypt` on the queue
  key, restricted by `kms:ViaService` to SQS. Deliberately not granted:
  `sqs:GetQueueUrl` (the URL is supplied, never discovered) and
  `sqs:ChangeMessageVisibility` (handling is parse-then-add, so no lease needs
  extending).
- ECR ingestion adds no inbound network path — the pod polls outward.
- In-cluster K8s auth only (ServiceAccount).
- RBAC least privilege in the CronJob namespace: `get` cronjobs, `create` and
  `list` jobs.
- Container runs as non-root with read-only filesystem.
- Distroless base image (no shell, minimal attack surface).

### NFR-3: Observability
- Structured JSON logging via `log/slog`, configurable level.
- DEBUG: ignored events (webhook and ECR), opt-out (no trigger declaration).
  INFO: job created, startup/shutdown, batch flush, empty-union no-op, accepted
  image push, consumer start/stop. WARN: invalid signatures, per-dependency
  resolution failures, postponed-too-long, unparseable queue message, failed
  message delete, repeated receive failures, unregistered dependency kind.
  ERROR: K8s failures.
- No sensitive data in logs (no secrets, no private key, no raw signatures).

### NFR-4: Reliability
- Single replica is an enforced invariant: `replicas: 1`, `strategy: Recreate`,
  no HorizontalPodAutoscaler. See ADR-0001.
- Lossy by design: pending batches are not persisted and triggers are not
  retried — dropped on shutdown, crash, or failed flush. The source CronJob
  schedule is the backstop. See ADR-0002.
- The delivery queue is transport, not durability: it survives a rollout, not an
  outage. Messages are deleted as soon as the dependency joins the batch and
  retention defaults to one hour. See ADR-0005.
- Graceful shutdown on SIGINT/SIGTERM. HTTP timeouts: read 10s, write 10s,
  idle 60s. The ECR consumer observes the same signal context and shutdown waits
  for it, bounded by the same 10s budget.
- Thread-safe batch collector using `sync.Mutex`.

### NFR-5: Deployment
- Helm chart with: Deployment (`replicas: 1`, `Recreate`), Service,
  ServiceAccount, Role, RoleBinding. The chart does **not** create a Secret;
  credentials (client ID, private key, webhook secret) are read from a single
  existing Secret provisioned out of band (e.g. External Secrets Operator), with
  configurable key names.
- Optional AWS infrastructure as a Terraform module under `terraform/`: an
  EventBridge rule on the default bus, the delivery queue and its dead-letter
  queue, a customer-managed KMS key (EventBridge cannot deliver to a queue
  encrypted with an AWS owned key), and the IAM role plus Pod Identity
  association. It ships no backend and no state — operators consume it.
- Multi-stage Docker build: `golang:1.26.5-alpine` builder,
  `distroless/static-debian12:nonroot` runtime. The distroless base ships CA roots,
  so TLS to `sqs.*` and `api.ecr.*` needs nothing extra.
- Multi-arch image (`linux/amd64`, `linux/arm64` via `docker buildx`): the
  builder stage runs on the native `$BUILDPLATFORM` (no QEMU emulation) and
  cross-compiles to `$TARGETOS`/`$TARGETARCH`.
- Static binary: `CGO_ENABLED=0`, stripped with `-ldflags="-s -w"`.
- Resource defaults: 50m/64Mi requests, 200m/128Mi limits.

### NFR-6: Testing
- 80%+ test coverage on internal packages. No test hits the network or sleeps on
  a real timer; all external I/O (GitHub, Kubernetes) is faked.
- **Seams:** the collector orchestrates three injected interfaces — `RunGate`
  (mutual-exclusion check over K8s), `Resolver` (dependencies → deduped
  dependents), and `JobCreator` — each faked in isolation. The resolver holds one
  `DependentsFetcher` per dependency kind; ingestion has one seam per source
  (`webhook.BatchAdder`, `awsecr.DependencyAdder`). SQS and the ECR API are faked
  with hand-written stubs in the same style as the GitHub `httptest` fixture.
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
| `RT_ECR_QUEUE_URL` | No | — | SQS queue URL for ECR image-push events; empty disables ECR ingestion |
| `RT_ECR_DEPENDENTS_TAG` | No | `renovate-trigger/dependents` | AWS tag key holding an ECR repository's dependents |
| `RT_AWS_REGION` | No | — | Region for the AWS clients; required in practice under EKS Pod Identity |

## Dependencies

| Dependency | Purpose |
|---|---|
| `k8s.io/client-go` | Kubernetes API client |
| `k8s.io/api` | Kubernetes types (Job, CronJob) |
| `k8s.io/apimachinery` | Kubernetes meta types |
| `golang-jwt/jwt` | Hand-rolled RS256 App JWT |
| `aws-sdk-go-v2/config` | AWS credential and region resolution (Pod Identity) |
| `aws-sdk-go-v2/service/sqs` | Delivery queue receive/delete/attributes |
| `aws-sdk-go-v2/service/ecr` | `ListTagsForResource` for the dependents tag |
| `log/slog` (stdlib) | Structured JSON logging |
| `crypto/hmac` (stdlib) | Webhook signature validation |
