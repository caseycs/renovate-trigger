# GitHub App setup

The service needs a **GitHub App** and its three credentials — **client ID**,
**private key** (PEM), and **webhook secret** — which you place in the existing
Kubernetes Secret the chart reads (`existingSecret`, see the
[README install steps](../README.md#3-configure-and-deploy)).

The [README](../README.md#1-create-a-github-app) covers creating the App by hand
in the UI. This doc covers automating it. There is **no `gh app create`**, and
private keys for an *existing* App can only be generated in the UI — so the only
automated path is GitHub's **App manifest flow**, which pre-fills every setting
and returns the private key programmatically at creation time.

## What the App needs (fixed)

- **Permissions:** `contents: read` — reads each repo's `renovate.trigger.json`
  and unlocks the `create` event.
- **Events:** subscribe to `create` (GitHub's tag/branch-creation event; the
  service acts only on tags).
- **Webhook:** one App-level webhook → the service's `/webhook`.

The manifest conversion returns `client_id`, `pem`, and `webhook_secret`, which
map to the Secret keys `github-client-id`, `github-app-private-key`, and
`webhook-secret`.

## Option A — `skyline` (recommended)

[`cased/skyline`](https://github.com/cased/skyline) is a small CLI that drives
the manifest flow end to end: it opens the pre-filled GitHub page, **captures
the callback for you** (via a one-off smee.io channel — no copying codes from
the address bar), exchanges it, and writes the credentials + PEM to disk.

> Third-party tool (MIT, installs via `pip`). It uses a transient smee.io channel
> only for the creation callback — your App's real webhook is whatever you set in
> `hook_attributes.url` below.

### 1. Install

```sh
curl -sSL https://raw.githubusercontent.com/cased/skyline/main/install.sh | bash
```

### 2. Write `skyline-config.json` for renovate-trigger

Edit `hook_attributes.url` to your Ingress host.

```json
{
  "name": "renovate-trigger",
  "url": "https://github.com/caseycs/renovate-trigger",
  "description": "Triggers Renovate on dependents when a dependency repo is tagged",
  "hook_attributes": { "url": "https://renovate-trigger.example.com/webhook" },
  "public": false,
  "default_permissions": { "contents": "read" },
  "default_events": ["create"]
}
```

### 3. Create the App

```sh
# Personal account — omit --org to be prompted, or pass your org:
skyline create --config skyline-config.json --org my-org
```

Click **Create GitHub App** on the page that opens. skyline then saves an `.env`
(`GITHUB_APP_ID`, `GITHUB_APP_CLIENT_ID`, `GITHUB_APP_WEBHOOK_SECRET`,
`GITHUB_APP_PRIVATE_KEY_PATH`) and the PEM (default `.github/app-private-key.pem`).

### 4. Create the credentials Secret

Match the key names the chart expects (`existingSecret.*Key` in your values):

```sh
set -a; . ./.env; set +a   # load GITHUB_APP_* from skyline's output

kubectl create secret generic renovate-trigger -n renovate \
  --from-literal=github-client-id="$GITHUB_APP_CLIENT_ID" \
  --from-file=github-app-private-key="$GITHUB_APP_PRIVATE_KEY_PATH" \
  --from-literal=webhook-secret="$GITHUB_APP_WEBHOOK_SECRET"
```

Delete `.env` and the PEM once the Secret exists, then
[install the App on your repos](#install-the-app-on-your-repositories) and deploy.

## Option B — Manifest flow by hand (no extra tooling)

If you'd rather not install skyline, run the same flow manually with a browser
and `gh`.

### 1. Save this as `app-manifest.html`

Edit the `EDIT_ME` values. For an org-owned App, change the form `action` to
`https://github.com/organizations/<ORG>/settings/apps/new?state=abc123`.

```html
<!doctype html>
<form id="f" method="post"
      action="https://github.com/settings/apps/new?state=abc123">
  <input type="hidden" name="manifest" id="manifest">
  <button type="submit">Create renovate-trigger GitHub App</button>
</form>
<script>
  document.getElementById("manifest").value = JSON.stringify({
    name: "renovate-trigger",                       // EDIT_ME (unique on GitHub)
    url: "https://github.com/caseycs/renovate-trigger",
    hook_attributes: {
      url: "https://renovate-trigger.example.com/webhook", // EDIT_ME (your Ingress)
      active: true
    },
    redirect_url: "http://localhost/",              // where GitHub returns the code
    public: false,
    default_permissions: { contents: "read" },
    default_events: ["create"]
  });
</script>
```

### 2. Create, then exchange the code

Open it in a browser logged into GitHub → submit → **Create GitHub App**. GitHub
redirects to `http://localhost/?code=<CODE>&state=abc123`; the page failing to
load is expected — **copy `code` from the address bar**. The code is single-use
and expires after **1 hour**.

```sh
gh api -X POST /app-manifests/<CODE>/conversions > app.json

jq -r .pem app.json > renovate-trigger.private-key.pem
chmod 600 renovate-trigger.private-key.pem

kubectl create secret generic renovate-trigger -n renovate \
  --from-literal=github-client-id="$(jq -r .client_id app.json)" \
  --from-file=github-app-private-key=renovate-trigger.private-key.pem \
  --from-literal=webhook-secret="$(jq -r .webhook_secret app.json)"

jq -r '"install at: " + .html_url + "/installations/new"' app.json
```

## Install the App on your repositories

Open `<html_url>/installations/new` (from skyline's output or `app.json`) and
install the App on the **dependency** repos whose tags should trigger Renovate.
This is a separate consent step — it cannot be scripted by the App owner. Then
deploy the chart ([README step 3](../README.md#3-configure-and-deploy)) and opt
repos in ([README step 4](../README.md#4-opt-repositories-in)).

## Limitations

- **No `gh app create`** — GitHub CLI has no native App-management command
  ([cli/cli#10536](https://github.com/cli/cli/discussions/10536)). The manifest
  flow is the only API-assisted path and still needs one browser click.
- **Existing-App private keys are UI-only** — there is no REST endpoint to
  generate or download a key for an App that already exists; the manifest
  conversion returns the PEM **only** at creation time.
- The conversion `code` expires after **1 hour** and can be used **once**.
- **Installing on repositories** requires the installer's consent and is not
  scriptable by the owner.

## References

- [cased/skyline](https://github.com/cased/skyline)
- [Registering a GitHub App from a manifest](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest)
- [Create a GitHub App from a manifest (REST)](https://docs.github.com/en/rest/apps/apps#create-a-github-app-from-a-manifest)
- [Managing private keys for GitHub Apps](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
