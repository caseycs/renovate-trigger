# Renovate-Trigger

A service that reacts to publication events — a GitHub tag, an ECR image push —
and triggers Renovate runs on the repositories that consume what was published.
This file defines the domain language; it is a glossary, not a spec.

## Dependency graph

**Dependency**:
A repository whose new versions we react to — a GitHub repository, or an ECR
container repository. When it publishes, its consumers may be out of date and
need a Renovate run.
_Avoid_: source repo, watched repo, upstream

**Dependency kind**:
Which system a Dependency lives in: GitHub or ECR. It decides how the Dependency
is identified (`owner/name` versus a repository ARN) and where its Trigger
declaration is read from (a file versus an AWS resource tag). Everything
downstream of Resolution is kind-agnostic.
_Avoid_: source type, provider, backend

**Dependent**:
A repository we run Renovate on when one of its dependencies publishes a new
tag, so it can pick up the new version.
_Avoid_: target repo, consumer, downstream

**Trigger declaration**:
A declaration a Dependency carries listing its Dependents. Its presence is the
repository's opt-in; its absence means "ignore my publications." It takes one form
per Dependency kind:
- GitHub — the `renovate.trigger.json` file on the default branch, shaped
  `{"tags": ["org/dependent-a", "org/dependent-b"]}`.
- ECR — the Dependents tag on the repository itself.
_Avoid_: config file, mapping file

**Tags** (the `tags` key):
The list of Dependents inside a GitHub Trigger declaration — the repositories to
run Renovate on when this Dependency is tagged. Named for the event that fires
them, not for their contents.

**Dependents tag** (`renovate-trigger/dependents`):
The AWS resource tag on an ECR repository declaring its Dependents, comma-separated:
`renovate-trigger/dependents = "org/app-a,org/app-b"`. The ECR form of a Trigger
declaration. AWS caps a tag value at 256 characters, which bounds an ECR repository
to roughly a dozen Dependents.
_Avoid_: label, annotation, marker

**On the word "tag"**:
It now means four different things — a git tag (the Tag event), the `tags` key
inside a GitHub Trigger declaration, a container image tag, and an AWS resource tag
(the Dependents tag). Always qualify it. Never write bare "tag" in code, comments,
or docs.

## Reacting

**Publication event**:
What a Dependency emits when it publishes something new — a Tag event on GitHub,
an Image push on ECR. The concrete version is irrelevant; any publication by an
opted-in Dependency reacts.
_Avoid_: notification, hook, trigger

**Tag event**:
A GitHub `create` event with `ref_type: "tag"`. The concrete git tag name and
version are irrelevant — any git tag on an opted-in Dependency reacts. One arrives
per git tag, but GitHub delivers none at all when a single push creates more than
three tags — see [README → Limitations](./README.md#limitations).
_Avoid_: release, push

**Image push**:
A successful `PUSH` in an ECR `ECR Image Action` event (`result: SUCCESS`).
Untagged (digest-only) pushes count, and one multi-architecture push emits several
events naming one repository, which collapse into a single Batch entry.
_Avoid_: image build, deploy, release

**Delivery queue**:
The SQS queue an EventBridge rule writes Image pushes to, which the pod long-polls.
It *delivers* publication events; it is not the Batch, and nothing accumulates in
it — a message is deleted as soon as its Dependency joins the Batch.
_Avoid_: batch queue, event queue, buffer

**Opt-in**:
A repository participates only when it is reachable and it carries a Trigger
declaration. On GitHub that means the App is installed on it and the file is
present; on ECR it means the pod's IAM role can read the repository's tags and the
Dependents tag is present. Reachability is the coarse gate; the declaration is the
per-repository detail.

## Batching

**Batch**:
The deduplicated set of Dependencies collected within one window, resolved
together into a single Renovate run. Deduplication is on the (kind, identifier)
pair, so the same name under two kinds is two Dependencies.
_Avoid_: queue, group

**Window**:
The fixed, tumbling interval that starts on the first Dependency added and fires
once. Dependencies arriving during the window join the current Batch; those
arriving after it starts a fresh one.
_Avoid_: debounce, sliding window

**Flush**:
Resolving a Batch — expanding each Dependency to its Dependents, taking the
deduplicated union, and creating one Renovate run for the result.

**Resolution**:
Expanding a Batch of Dependencies into the union of their Dependents by reading
each one's Trigger declaration. Happens at Flush time, not on the request path.

## Execution

**Renovate run**:
A Kubernetes Job that executes Renovate against a set of Dependents. Runs are
cloned from the source CronJob's template; the scheduled CronJob produces its own
runs on its schedule.

**Active Renovate run**:
Any Renovate run currently executing — either one of ours (our `managed-by`
label) or one the source CronJob spawned (owned by the CronJob). A Job that is
neither Complete nor Failed.

**Mutual exclusion**:
The invariant that at most one Renovate run executes at a time. A Flush that
finds an Active Renovate run is postponed, not run concurrently.
_Avoid_: locking, serialization
