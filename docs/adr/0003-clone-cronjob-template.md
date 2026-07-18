# Clone the Renovate CronJob template at runtime

To create a Renovate run, we read the existing Renovate CronJob at flush time,
deep-copy its `jobTemplate`, inject `RENOVATE_REPOSITORIES`, and create a one-off
Job — rather than baking a full Renovate Job spec into this app's Helm chart.

The CronJob is the single source of truth for the Renovate image, args, volumes,
secrets, resource limits, and `RENOVATE_*` config. Cloning it means we inherit
every change to that spec for free; duplicating it into a second chart would rot
out of sync. We accept the runtime coupling this creates.

## Consequences

- The app needs `get cronjobs` RBAC in the CronJob's namespace, and a
  missing/renamed CronJob makes every flush fail — so startup does a `get` on the
  CronJob and exits if it is absent, turning a config error into a loud
  crash-loop instead of silent lost triggers.
- We clone whatever the CronJob looks like at flush time, including a
  suspended/half-edited spec.
