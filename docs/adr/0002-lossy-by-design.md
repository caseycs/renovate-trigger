# Lossy by design

A pending batch is not durable and triggers are never retried. It is lost on
graceful shutdown (the timer is cancelled, not flushed), on crash (state is
in-memory), and on a failed flush (the error is logged and discarded). We chose
this over persistence or a retry queue.

The rationale is that a Renovate run is idempotent and the source CronJob still
runs on its own schedule. A dropped trigger only delays a dependency scan until
the next tag or the next scheduled run — never causes incorrect state. That
makes durability machinery (a queue, a persistent store, retry/backoff) cost
without meaningful benefit.

## Consequences

- Deploys are the frequent, predictable loss vector — a rollout drops the
  in-flight window. This is accepted; the CronJob schedule is the backstop.
- Debugging a "missing" run means checking logs, not a dead-letter queue.
