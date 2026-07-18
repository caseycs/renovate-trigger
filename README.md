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

## Prerequisites

- A **Renovate CronJob** already deployed in the cluster — its `jobTemplate` is
  the template this service clones.
- A **GitHub App** with `Contents: read`, subscribed to `create` events, its
  webhook pointing at this service, installed on the repos that opt in. You need
  its **client ID**, a **private key** (PEM), and the **webhook secret**.

## Install (Helm)

```sh
helm install renovate-trigger ./chart \
  --namespace renovate-trigger --create-namespace \
  --set config.cronjob.name=renovate \
  --set config.cronjob.namespace=renovate \
  --set github.clientId=Iv23liXXXXXXXXXXXXXX \
  --set-file github.privateKey=./renovate-trigger.private-key.pem \
  --set webhook.secret="$WEBHOOK_SECRET"
```

Then point the GitHub App's webhook URL at the Service (via your Ingress).

> **Status:** the chart in `chart/` still reflects the pre-redesign layout. The
> `github.*` / `webhook.secret` values, the App-private-key Secret mount, and the
> `list jobs` RBAC rule land with the implementation follow-up (see the deferred
> work in `REQUIREMENTS.md`). Until then, treat the command above as the target
> install shape.

## Configuration

All operational config is via `RT_*` environment variables (set by the chart);
the dependency graph itself lives in per-repo `renovate.trigger.json` files. See
the [configuration reference](./REQUIREMENTS.md#configuration-reference).

## Build

Multi-arch image, cross-compiled on the native host arch (no emulation):

```sh
docker buildx build --platform linux/amd64,linux/arm64 -t renovate-trigger:latest .
```
