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
- An **existing Kubernetes Secret** with the App credentials — the chart does
  **not** create one; provision it out of band (e.g. with External Secrets
  Operator, or by rendering the Secret / an `ExternalSecret` through the chart's
  `extraObjects:` value). A single Secret with three keys: the App **client ID**,
  the App **private key** (PEM), and the **webhook secret**.

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
  strong random **Secret**.
- **Generate a private key** and download the `.pem`.
- Note the App's **Client ID**.

Store these three — **client ID**, **private key** (PEM), and **webhook secret**
— in the existing credentials Secret (see step 3).

> Prefer to skip the manual clicking? [`docs/github-app-setup.md`](./docs/github-app-setup.md)
> automates this with [`cased/skyline`](https://github.com/cased/skyline) (one
> command creates the App and writes the credentials, including the private key),
> with a no-tooling manifest-flow fallback.

### 2. Install the App on your repositories

Install the App on the **dependency** repos whose tags should trigger Renovate.
Only installed repos deliver events and are readable — App-installed + a
`renovate.trigger.json` present is the opt-in.

### 3. Configure and deploy

Put your settings in a values file — `renovate-trigger.values.yaml`. Install into
the **same namespace as the Renovate CronJob**; the service is co-located with
it, so the CronJob namespace is just the release namespace.

```yaml
config:
  cronjob:
    name: renovate            # source Renovate CronJob (its jobTemplate is cloned)
    # namespace: ...          # only if it differs from the release namespace

# Existing Secret holding the App credentials (created out of band, e.g. via
# External Secrets Operator) — one Secret, three keys. Override the key names to
# match yours.
existingSecret:
  name: renovate-trigger
  clientIdKey: github-client-id
  privateKeyKey: github-app-private-key
  webhookSecretKey: webhook-secret

# Expose /webhook at the host used for the App's webhook URL (step 1).
# Enable ONE of the following, or wire your own routing instead.
ingress:
  enabled: true
  host: renovate-trigger.example.com
  className: nginx
  tls:
    enabled: true

# Traefik alternative (leave `ingress` disabled if you use this):
# ingressRoute:
#   enabled: true
#   host: renovate-trigger.example.com
#   entryPoint: websecure
#   labels:
#     nobi.life/traefik-scope: external
```

Then install — the chart and image are published to GHCR:

<!-- x-release-please-start-version -->
```sh
helm install renovate-trigger oci://ghcr.io/caseycs/charts/renovate-trigger \
  --version 0.1.0 --namespace renovate \
  -f renovate-trigger.values.yaml
```
<!-- x-release-please-end -->

To install from a local checkout instead, swap the chart reference for `./chart`.

The service starts only if the config is valid — a missing key in the Secret or
an unreachable CronJob crash-loops the pod at boot (fail-loud by design).

### 4. Opt repositories in

For each dependency repo, add a `renovate.trigger.json` on its default branch —
see [Opting a repository in](#opting-a-repository-in) above.

## Configuration

All operational config is via `RT_*` environment variables (set by the chart);
the dependency graph itself lives in per-repo `renovate.trigger.json` files. See
the [configuration reference](./REQUIREMENTS.md#configuration-reference).

