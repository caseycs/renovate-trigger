# ECR image pushes as a second Dependency source, via EventBridge to SQS

An ECR repository is a Dependency: when a new image lands in it, the repositories
whose manifests pin that image are out of date. ECR has no webhooks, so an
EventBridge rule on the default bus delivers `ECR Image Action` / `PUSH` events to
an SQS queue that the pod long-polls. An ECR repository declares its Dependents in
an AWS resource tag on the repository itself — `renovate-trigger/dependents =
"org/app-a,org/app-b"` — which is the registry analogue of a `renovate.trigger.json`
Trigger declaration, and decentralized for the same reason (ADR-0004): the
declaration belongs to whoever owns the thing being published.

A queue rather than an HTTP push because a queue needs no public ingress, no second
authentication scheme, and no second deploy artifact. A pull consumer also suits a
service that already owns its own in-memory Batch: nothing arrives that it did not
ask for.

The Batch element becomes a typed `(kind, name)` pair and the Resolver dispatches to
one fetcher per kind. A third source is then a fetcher and a kind, not a new
pipeline. For ECR the name is the repository ARN: `ecr:ListTagsForResource` takes an
ARN, the ARN is globally unique so two repositories named `api` in different
accounts stay distinct, and nobody ever types it — the tag is machine-read end to
end. The event's `resources` array is empty for this detail-type, so the ARN is
constructed from the event's account, region, and repository name.

## Considered options

- **EventBridge API destination straight to `/webhook`**: rejected. API destinations
  authenticate through an EventBridge Connection (API key, Basic, OAuth) and cannot
  produce a GitHub-style HMAC-SHA256 over the request body, so FR-1's signature check
  would have to be bypassed or a second, differently-authenticated route added. That
  is a security regression to save a queue, and it makes the trigger path depend on
  ingress being reachable.
- **EventBridge → Lambda → `/webhook`**: rejected. The Lambda would exist only to
  compute an HMAC, and it brings a second deploy artifact, runtime, and log group
  into a repo whose release pipeline publishes exactly one image and one chart —
  plus a VPC and NAT question the moment the ingress is private.
- **Polling `ecr:DescribeImages` on a timer**: rejected. With no event, the service
  would have to remember what it saw last time, which contradicts ADR-0002's
  no-persistence stance, and it costs one call per repository per interval.
- **A central mapping of ECR repository to Dependents** (ConfigMap, Helm values, or
  a Terraform allowlist): rejected for exactly the reasons ADR-0004 gives. A
  Terraform-side allowlist is the same central allowlist wearing HCL.
- **Resolving the Dependents tag on the ingestion path**: rejected. `CONTEXT.md`
  puts Resolution at Flush time, and resolving early would do AWS I/O per message,
  defeat deduplication (a multi-arch push emits several events for one repository),
  and leave a postponed Batch holding stale Dependents instead of re-reading them.
- **SQS-managed SSE on the queue**: not available. EventBridge documents that it
  "does not support using Amazon SQS queues that are encrypted with an AWS owned
  key. This includes targets, as well as Amazon SQS queues specified as dead-letter
  queues for targets." A customer-managed key is therefore required, not preferred.

## Consequences

- **The queue is transport, not durability, and ADR-0002 still holds.** This is the
  tension worth naming: SQS *is* a durable queue with redelivery, which is close to
  the retry queue ADR-0002 said this service would not build. We put one in front of
  the pipeline and then deliberately discard its durability — the message is deleted
  as soon as the Dependency joins the Batch, and retention defaults to one hour. The
  Batch is still in-memory, still single-replica, still lost on restart. A long
  retention would replay a fortnight of stale pushes into one enormous Renovate run
  after an outage, which is the wrong behaviour, loudly.
- **ADR-0001 is reinforced, not superseded.** SQS distributes messages across
  consumers, so a second replica would split a Window exactly as duplicate webhook
  delivery would. The single-replica invariant transfers unchanged.
- **ADR-0002's "checking logs, not a dead-letter queue" is amended.** There is a
  dead-letter queue now, but it is a poison-message and undeliverable-event sink, not
  a retry queue. A message body the app cannot parse is deliberately *not* deleted,
  so the redrive policy surfaces it there instead of the app hiding it; a
  well-formed event we simply do not act on is deleted immediately.
- The service gains AWS credentials and a second egress path (SQS and the ECR API) —
  new attack surface, mirroring ADR-0004's first consequence. It gains no new
  inbound path at all.
- **Anyone holding `ecr:TagResource` can nominate Dependents.** On GitHub, opting in
  requires write access to the Dependency's default branch; here it requires a
  permission most CI roles already hold, often via
  `AmazonEC2ContainerRegistryPowerUser`. The ceiling is bounded — Renovate runs with
  the CronJob's own credentials and only opens pull requests — but it is a real
  asymmetry, and the mitigation is an SCP denying `ecr:TagResource` on the
  `renovate-trigger/dependents` tag key to all but a platform role.
- AWS tag values cap at 256 characters, so an ECR repository can declare roughly a
  dozen Dependents. There is no equivalent ceiling on the GitHub side. If it bites,
  the escape hatch is numbered keys; that is deliberately not built.
- Terraform enters the repo, under `terraform/`. It carries no state and no backend:
  it is a module operators consume, not a root configuration.
- Credentials come from EKS Pod Identity. `AWS_EC2_METADATA_DISABLED=true` is set
  whenever ECR is enabled, so a misconfigured association fails at the boot-time
  queue check rather than silently running with the node instance profile.
- Single-region. ECR emits to the default bus in the pushing region and an
  EventBridge target must be same-region, so a multi-region estate needs one module
  instance and one queue per region.
- Best effort twice over: AWS states ECR events are emitted on a best-effort basis,
  and the rule is deliberately loose — it does not filter on `image-tag`, because
  that field is absent for untagged pushes. Replicated images and referrer restores
  carry different detail-types and do not fire the rule.
- The GitHub App credentials stay required even for a deployment that only uses ECR.
  Relaxing that touches config validation, the chart's `existingSecret` contract, the
  webhook route, and the install story; it is a known follow-up, not part of this
  change.
- Cost is about a dollar a month, effectively all of it the KMS key. Default-bus
  rules on AWS-service events are free and one long-polling consumer stays inside
  the SQS free tier.
