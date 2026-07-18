# Decentralized trigger config via a GitHub App

Repositories declare which dependents to Renovate in a `renovate.trigger.json`
file on their own default branch (`{"tags": ["org/app-a", ...]}`), rather than in
a central ConfigMap or an `RT_REPOS` allowlist. A single GitHub App both delivers
the `create` webhooks and grants read access to those files.

Decentralized declaration puts the dependency graph under the control of the repo
owners who understand it, and makes the graph self-documenting and versioned
alongside the code. Routing both webhook delivery and content reads through one
App collapses two integrations into one: any repo whose events we receive is
guaranteed App-installed, so the token can always read its file — no skew between
"who sends events" and "who we can read."

## Considered options

- **Central ConfigMap mapping** (`dependency -> [dependents]`): rejected — it
  centralizes knowledge that belongs to repo owners and drifts from reality. It
  was briefly chosen during design before the decentralized model replaced it.
- **`RT_REPOS` env allowlist**: rejected — a flat list can't express a graph, and
  it made "deployed but silently doing nothing" possible.

## Consequences

- The app needs GitHub read credentials and network egress to the GitHub API — a
  new secret and attack surface it previously did not have.
- Auth uses the App **client ID** in a hand-rolled RS256 JWT (client ID is
  GitHub's recommended, future-proof identifier; the `ghinstallation` library only
  accepts the numeric App ID). The installation is discovered per-repository at
  flush time, so no installation ID is configured and multiple installations work.
- Presence of the file is the opt-in. A tagged repo with no file is a normal
  opt-out (logged at DEBUG), not an error.
