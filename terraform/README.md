# renovate-trigger: AWS ECR event source

Terraform for the optional ECR event source. It creates an EventBridge rule that
selects successful ECR image pushes, an SQS delivery queue (plus a dead-letter
queue and a KMS key), and the IAM role the pod assumes through EKS Pod Identity.

**This directory is a module, not a root configuration.** It ships no `backend`
block and no `*.tfbackend` — state and backend belong to your own root module.
`examples/complete/` is a root module that exists only so CI can `validate` it
without credentials.

See [`docs/adr/0005-ecr-image-pushes-via-eventbridge-to-sqs.md`](../docs/adr/0005-ecr-image-pushes-via-eventbridge-to-sqs.md)
for why it is shaped this way, and the repository [`README.md`](../README.md#aws-ecr-image-pushes-as-a-second-event-source)
for the end-to-end setup.

## Usage

<!-- x-release-please-start-version -->
```hcl
module "renovate_trigger_ecr" {
  source = "git::https://github.com/caseycs/renovate-trigger.git//terraform?ref=v0.3.1"

  cluster_name         = "platform-eu-west-1"
  namespace            = "renovate"
  service_account_name = "renovate-trigger"
}
```
<!-- x-release-please-end -->

Then wire the chart to it:

```sh
terraform output -raw helm_values > renovate-trigger.aws.values.yaml
```

## Prerequisites

The **`eks-pod-identity-agent`** addon must be installed on the cluster. Pod
Identity does nothing without it, and the pod will crash-loop at its boot-time
queue check. Set `manage_pod_identity_agent = true` to have this module create the
addon, but prefer leaving it to whatever provisions the cluster.

## Things worth knowing before you apply

**A customer-managed KMS key is required, not optional.** EventBridge documents
that it "does not support using Amazon SQS queues that are encrypted with an AWS
owned key. This includes targets, as well as Amazon SQS queues specified as
dead-letter queues for targets." That rules out SQS-managed SSE, so this module
creates a key (or takes one via `kms_key_arn`). It is essentially the whole running
cost: about $1/month. Default-bus rules on AWS-service events are free, and one
consumer long-polling at 20s stays inside the SQS free tier.

**Every image push in the account reaches the queue by default.** That is
deliberate: a repository opts in by carrying the `renovate-trigger/dependents` tag,
not by appearing in a Terraform allowlist — the same decentralization ADR-0004 chose
for GitHub. A repository without the tag is dropped after one cheap tag read. Use
`dependency_repository_name_prefixes` only to keep a genuinely noisy high-volume CI
repository out of the queue. EventBridge patterns cannot match resource tags, which
is why the tag read happens later, at flush.

**Anyone with `ecr:TagResource` can nominate dependents.** On GitHub, opting a
dependency in requires write access to its default branch. Here it requires a
permission many CI roles already hold, often via
`AmazonEC2ContainerRegistryPowerUser` — so anyone with it can point Renovate at
arbitrary repositories. The ceiling is bounded (Renovate runs with the CronJob's own
credentials and only opens pull requests), but if that matters, deny the tag key
outside a platform role with an SCP:

```json
{
  "Sid": "OnlyPlatformMayNominateDependents",
  "Effect": "Deny",
  "Action": ["ecr:TagResource", "ecr:UntagResource"],
  "Resource": "*",
  "Condition": {
    "ForAnyValue:StringEquals": { "aws:TagKeys": ["renovate-trigger/dependents"] },
    "StringNotLike": { "aws:PrincipalArn": "arn:aws:iam::*:role/platform-admin" }
  }
}
```

**Retention is one hour, not fourteen days.** The queue exists to survive a
rollout, not an outage. renovate-trigger is lossy by design (ADR-0002) and the
Renovate CronJob schedule is the backstop, so a long retention would only replay a
fortnight of stale pushes into one enormous run after an outage.

**Single region.** ECR emits to the default bus in the pushing region and an
EventBridge target must be same-region, so a multi-region estate needs one module
instance per region.

**The Pod Identity association pins one namespace and one ServiceAccount name.**
Renaming the Helm release, or overriding `serviceAccount.name`, means re-applying.

## Inputs

| Name | Type | Default | Description |
|---|---|---|---|
| `cluster_name` | `string` | — | **Required.** EKS cluster for the Pod Identity association. |
| `name_prefix` | `string` | `renovate-trigger` | Prefix for created resource names. |
| `namespace` | `string` | `renovate` | Namespace of the Helm release. |
| `service_account_name` | `string` | `renovate-trigger` | ServiceAccount the pod runs as. |
| `dependents_tag_key` | `string` | `renovate-trigger/dependents` | Tag key declaring dependents. Must match the chart's `config.ecr.dependentsTag`. |
| `dependency_repository_name_prefixes` | `list(string)` | `[]` | Narrow the rule by repository-name prefix. Empty means every push fires. |
| `dependency_repository_arns` | `list(string)` | `[]` | Repositories the pod may read tags from. Empty means all in this account and region. |
| `create_iam_role` | `bool` | `true` | Create the role and association. False when IAM is managed elsewhere. |
| `manage_pod_identity_agent` | `bool` | `false` | Create the `eks-pod-identity-agent` addon. |
| `permissions_boundary_arn` | `string` | `null` | Permissions boundary for the role. |
| `kms_key_arn` | `string` | `null` | Use an existing CMK instead of creating one. |
| `kms_key_deletion_window_in_days` | `number` | `7` | Waiting period before a created key is deleted. |
| `message_retention_seconds` | `number` | `3600` | Delivery queue retention. |
| `visibility_timeout_seconds` | `number` | `60` | Delivery queue visibility timeout. |
| `max_receive_count` | `number` | `5` | Receives before a message is dead-lettered. |
| `dlq_message_retention_seconds` | `number` | `1209600` | Dead-letter queue retention. |
| `event_target_max_retry_attempts` | `number` | `3` | EventBridge delivery retries. |
| `event_target_max_event_age_seconds` | `number` | `900` | EventBridge retry window. |
| `create_alarms` | `bool` | `false` | Create the queue-age and dead-letter alarms. |
| `alarm_sns_topic_arns` | `list(string)` | `[]` | Topics the alarms notify. Required when `create_alarms`. |
| `queue_age_alarm_threshold_seconds` | `number` | `900` | Oldest-message age that means the consumer is stuck. |
| `resource_tags` | `map(string)` | `{}` | Extra AWS tags merged into everything created. |

`receive_wait_time_seconds` is fixed at 20 — SQS's maximum long poll, and the whole
point of the design. A lower value is a cost bug, not a latency win.

## Outputs

| Name | Description |
|---|---|
| `helm_values` | Rendered YAML fragment wiring the chart to this module. |
| `delivery_queue_url` | Set as the chart's `config.ecr.queueUrl`. |
| `region` | Set as the chart's `config.ecr.region`. |
| `delivery_queue_arn`, `delivery_queue_name` | Delivery queue identifiers. |
| `dead_letter_queue_url`, `dead_letter_queue_arn` | Dead-letter queue identifiers. |
| `role_arn`, `role_name` | Consumer role. Null when `create_iam_role` is false. |
| `event_rule_arn` | The EventBridge rule. |
| `kms_key_arn` | Key encrypting the queues. |
| `dependents_tag_key` | Echo of the input, so chart and module cannot drift. |

## Alarms

Off by default; turn them on. renovate-trigger is lossy by design, so nothing else
in the system complains when a trigger is dropped — a dead or wedged consumer is
otherwise completely invisible.

- **`<prefix>-ecr-image-push-not-consumed`** — the oldest queued event is older than
  `queue_age_alarm_threshold_seconds`. This is the one that matters.
- **`<prefix>-ecr-image-push-dead-lettered`** — anything in the dead-letter queue is
  either an event body the app could not parse or an event EventBridge could not
  deliver at all. Both want a human.
