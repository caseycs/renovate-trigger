# renovate-trigger

Reacts to GitHub tag events and runs Renovate on the repositories that **consume**
the newly-tagged code. When a **dependency** repo publishes a tag, the service
reads that repo's `renovate.trigger.json` to find its **dependents**, batches them
over a short window, and creates a one-off Kubernetes `Job` cloned from an
existing Renovate `CronJob` — injecting the dependents via `RENOVATE_REPOSITORIES`.

A single **GitHub App** both delivers the webhooks and grants read access to the
trigger files.

- Domain vocabulary → [`CONTEXT.md`](./CONTEXT.md)
- Requirements & config reference → [`REQUIREMENTS.md`](./REQUIREMENTS.md)
- Diagrams → [`WORKFLOWS.md`](./WORKFLOWS.md)
- Design decisions → [`docs/adr/`](./docs/adr/)

## Opting a repository in

Add `renovate.trigger.json` to the **default branch** of a repository whose tags
should trigger Renovate on its consumers:

```json
{ "tags": ["org/dependent-a", "org/dependent-b"] }
```

A tag on a repo with no such file (or without the App installed) is ignored.

## Installation

### Prerequisites

- A **Renovate `CronJob`** already deployed in the cluster — its `jobTemplate`
  is what this service clones for each run.
- Cluster access (`kubectl`/`helm`) to the CronJob's namespace.
- A way to expose the service's `/webhook` endpoint to GitHub (an Ingress).

### 1. Create a GitHub App

In **Settings → Developer settings → GitHub Apps → New GitHub App** (org- or
user-owned):

- **Permissions → Repository → Contents: `Read-only`.** This is used both to read
  each repo's `renovate.trigger.json` *and* to unlock the `Create` event below —
  `Create` only becomes selectable once Contents is granted.
- **Subscribe to events → check `Create`.** This is GitHub's event for tag (and
  branch) creation; the service ignores branches and acts only on tags. There is
  **one** App-level webhook — you never configure per-repo webhooks.
- **Webhook:** set **Active**, **URL** = `https://<your-host>/webhook`, and a
  strong random **Secret** (save it — it becomes `webhookSecret`).
- **Generate a private key** and download the `.pem` (save it).
- Note the App's **Client ID**.

### 2. Install the App on your repositories

Install the App on the **dependency** repos whose tags should trigger Renovate.
Only installed repos deliver events and are readable — App-installed + a
`renovate.trigger.json` present is the opt-in.

### 3. Deploy with Helm

Install into the **same namespace as the Renovate CronJob** — the service is
co-located with it, so the CronJob namespace is just the release namespace. The
chart and image are published to GHCR (`ghcr.io/caseycs/charts/renovate-trigger`
and `ghcr.io/caseycs/renovate-trigger`):

```sh
helm install renovate-trigger oci://ghcr.io/caseycs/charts/renovate-trigger \
  --version 0.1.0 \
  --namespace renovate \
  --set config.cronjob.name=renovate \
  --set github.clientId=Iv23liXXXXXXXXXXXXXX \
  --set-file github.privateKey=./renovate-trigger.private-key.pem \
  --set webhookSecret="$WEBHOOK_SECRET"
```

- `config.cronjob.name` — the source Renovate CronJob (namespace defaults to the
  release namespace; override with `config.cronjob.namespace` only if it differs).
- `github.clientId` — the App Client ID from step 1.
- `github.privateKey` — the `.pem` from step 1 (via `--set-file`).
- `webhookSecret` — the webhook secret from step 1.

To install from a local checkout instead, swap the chart reference for `./chart`.

The service starts only if all of the above are valid — a missing/unparseable
key or an unreachable CronJob crash-loops the pod at boot (fail-loud by design).

### 4. Point the webhook at the service

Expose the `Service` (port 8080, path `/webhook`) through your Ingress at the
host you used for the App's webhook URL in step 1.

### 5. Opt repositories in

For each dependency repo, add a `renovate.trigger.json` on its default branch —
see [Opting a repository in](#opting-a-repository-in) above.

## Configuration

All operational config is via `RT_*` environment variables (set by the chart);
the dependency graph itself lives in per-repo `renovate.trigger.json` files. See
the [configuration reference](./REQUIREMENTS.md#configuration-reference).

## Releasing

Commit to `main` freely; releases are cut on demand.
[Release Drafter](https://github.com/release-drafter/release-drafter) keeps a
**draft GitHub Release** continuously up to date — categorising merged PRs and
resolving the next `vX.Y.Z` from their labels (Conventional-Commit titles are
auto-labelled). When you want to ship, **publish that draft release** (tweak the
tag first if you want a different bump). Publishing fires the `release` workflow,
which builds the multi-arch image and packages/pushes the Helm chart to GHCR
under one shared version (image tag = chart `version` = `appVersion`, taken from
the release tag).

The chart in git keeps a placeholder `0.1.0`; the published artifacts are
stamped with the release tag at package time, so no in-repo version bump is
needed.

For a local multi-arch build (dev only; releases go through CI):

```sh
docker buildx build --platform linux/amd64,linux/arm64 -t renovate-trigger:latest .
```
