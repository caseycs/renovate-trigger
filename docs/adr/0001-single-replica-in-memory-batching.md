# Single-replica, in-memory batching

The batch of tagged dependencies is held in process memory (a map guarded by a
mutex) with a single timer. This is correct only when exactly one replica runs:
with two or more, a GitHub webhook lands on one arbitrary pod, so tags for the
same window would split across pods into separate, partial Renovate runs.

We accept single-replica as an enforced invariant — the Deployment pins
`replicas: 1` with `strategy: Recreate` (so a rollout never briefly runs two
collecting pods) — rather than build distributed batching (shared store or
leader election). Horizontal scale buys nothing for a periodic maintenance
trigger, and the machinery would dwarf the tool.

## Consequences

- No HorizontalPodAutoscaler may be attached. Scaling out silently breaks
  batching and mutual exclusion; it is a design error, not a capacity knob.
- Batch state is lost on restart/crash (see [0002](./0002-lossy-by-design.md)).
