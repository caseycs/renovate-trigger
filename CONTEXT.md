# Renovate-Trigger

A service that reacts to GitHub tag events and triggers Renovate runs on the
repositories that consume the newly-tagged code. This file defines the domain
language; it is a glossary, not a spec.

## Dependency graph

**Dependency**:
A repository whose new tags we react to. When it publishes a tag, its consumers
may be out of date and need a Renovate run.
_Avoid_: source repo, watched repo, upstream

**Dependent**:
A repository we run Renovate on when one of its dependencies publishes a new
tag, so it can pick up the new version.
_Avoid_: target repo, consumer, downstream

**Trigger declaration** (`renovate.trigger.json`):
A file a Dependency keeps on its default branch declaring its Dependents. Its
presence is the repository's opt-in; its absence means "ignore my tags." Shape:
`{"tags": ["org/dependent-a", "org/dependent-b"]}`.
_Avoid_: config file, mapping file

**Tags** (the `tags` key):
The list of Dependents inside a Trigger declaration — the repositories to run
Renovate on when this Dependency is tagged. Named for the event that fires them,
not for their contents.

## Reacting

**Tag event**:
A GitHub `create` event with `ref_type: "tag"`. The concrete tag name and
version are irrelevant — any tag on an opted-in Dependency reacts.
_Avoid_: release, push

**Opt-in**:
A repository participates only when both are true: the GitHub App is installed
on it, and it carries a Trigger declaration. The App install is the coarse gate;
the declaration is the per-repository detail.

## Batching

**Batch**:
The deduplicated set of Dependencies collected within one window, resolved
together into a single Renovate run.
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
