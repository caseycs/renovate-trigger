# GitHub App setup

The service needs a **GitHub App** and its three credentials — **client ID**,
**private key** (PEM), and **webhook secret** — which you place in the existing
Kubernetes Secret the chart reads (`existingSecret`, see the
[README install steps](../README.md#3-configure-and-deploy)).

The [README](../README.md#1-create-a-github-app) covers creating the App by hand
in the UI. This doc covers the **semi-automated alternative**: GitHub's App
**manifest flow**, which pre-fills every setting and hands you the private key
programmatically. It is the only API-assisted path — there is no `gh app create`,
and private keys for an *existing* App can only be generated in the UI.

## What the App needs (fixed)

- **Permissions:** `Contents: read` — reads each repo's `renovate.trigger.json`
  and unlocks the `create` event.
- **Events:** subscribe to `create` (GitHub's tag/branch-creation event; the
  service acts only on tags).
- **Webhook:** one App-level webhook → the service's `/webhook`.

The manifest conversion returns `client_id`, `pem`, and `webhook_secret` — these
map 1:1 to the Secret keys `github-client-id`, `github-app-private-key`, and
`webhook-secret`.

## Manifest flow (one browser click)

### 1. Save this as `app-manifest.html`

Edit the three `EDIT_ME` values. For an org-owned App, change the form `action`
to `https://github.com/organizations/<ORG>/settings/apps/new?state=abc123`.

```html
<!doctype html>
<form id="f" method="post"
      action="https://github.com/settings/apps/new?state=abc123">
  <input type="hidden" name="manifest" id="manifest">
  <button type="submit">Create renovate-trigger GitHub App</button>
</form>
<script>
  document.getElementById("manifest").value = JSON.stringify({
    name: "renovate-trigger",                       // EDIT_ME (must be unique on GitHub)
    url: "https://github.com/caseycs/renovate-trigger",
    hook_attributes: {
      url: "https://renovate-trigger.example.com/webhook", // EDIT_ME (your Ingress host)
      active: true
    },
    redirect_url: "http://localhost/",              // where GitHub returns the code
    public: false,
    default_permissions: { contents: "read" },
    default_events: ["create"]
  });
</script>
```

GitHub generates the webhook secret for you, so the manifest omits it.

### 2. Create the App

Open `app-manifest.html` in a browser **logged into GitHub**, submit, review the
pre-filled registration, and click **Create GitHub App**.

GitHub then redirects to `http://localhost/?code=<CODE>&state=abc123`. The page
failing to load is expected — **copy the `code` value from the address bar**.

### 3. Exchange the code for the credentials

The code is single-use and expires after **1 hour**. `gh api` reuses your `gh`
auth:

```sh
gh api -X POST /app-manifests/<CODE>/conversions > app.json

jq -r .pem app.json > renovate-trigger.private-key.pem
chmod 600 renovate-trigger.private-key.pem

jq -r '"client_id:      " + .client_id,
       "webhook_secret: " + .webhook_secret,
       "install at:     " + .html_url + "/installations/new"' app.json
```

### 4. Create the credentials Secret

Match the key names the chart expects (`existingSecret.*Key` in your values):

```sh
kubectl create secret generic renovate-trigger -n renovate \
  --from-literal=github-client-id="$(jq -r .client_id app.json)" \
  --from-file=github-app-private-key=renovate-trigger.private-key.pem \
  --from-literal=webhook-secret="$(jq -r .webhook_secret app.json)"
```

Then delete the local `app.json` and PEM once the Secret exists.

### 5. Install the App on your repositories

Open `<html_url>/installations/new` (printed in step 3) and install the App on
the **dependency** repos whose tags should trigger Renovate. This is a separate
consent step — it cannot be scripted by the App owner. Then deploy the chart
(README step 3) and opt repos in (README step 4).

## Limitations

- **No `gh app create`** — GitHub CLI has no native App-management command
  ([cli/cli#10536](https://github.com/cli/cli/discussions/10536)). The manifest
  flow is the only API-assisted path and still requires one browser interaction.
- **Existing-App private keys are UI-only** — there is no REST endpoint to
  generate or download a key for an App that already exists; the manifest
  conversion returns the PEM **only** at creation time.
- The conversion `code` expires after **1 hour** and can be used **once**.
- **Installing on repositories** requires the installer's consent and is not
  scriptable by the owner.

## References

- [Registering a GitHub App from a manifest](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest)
- [Create a GitHub App from a manifest (REST)](https://docs.github.com/en/rest/apps/apps#create-a-github-app-from-a-manifest)
- [Managing private keys for GitHub Apps](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
- [Is it possible to create a GitHub app using the GitHub CLI? (cli/cli#10536)](https://github.com/cli/cli/discussions/10536)
