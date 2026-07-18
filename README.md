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

Install into the **same namespace as the Renovate CronJob** — the service is
co-located with it and clones its `jobTemplate`, so the CronJob namespace is just
the release namespace.

The chart is published as an OCI artifact to GHCR
(`ghcr.io/caseycs/charts/renovate-trigger`); the image lives at
`ghcr.io/caseycs/renovate-trigger`. Install a released version directly:

```sh
helm install renovate-trigger oci://ghcr.io/caseycs/charts/renovate-trigger \
  --version 0.1.0 \
  --namespace renovate \
  --set config.cronjob.name=renovate \
  --set github.clientId=Iv23liXXXXXXXXXXXXXX \
  --set-file github.privateKey=./renovate-trigger.private-key.pem \
  --set webhookSecret="$WEBHOOK_SECRET"
```

To install from a local checkout instead, swap the chart reference for `./chart`.

Then point the GitHub App's webhook URL at the Service (via your Ingress).

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
