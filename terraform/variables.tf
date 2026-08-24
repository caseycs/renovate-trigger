variable "name_prefix" {
  type        = string
  description = "Prefix for the names of the queue, rule, role, and KMS alias this module creates."
  default     = "renovate-trigger"

  validation {
    # SQS queue names cap at 80 characters and rule names at 64. Capping the
    # prefix at 40 leaves room for the longest suffix ("-ecr-image-push-dlq").
    condition     = can(regex("^[a-z][a-z0-9-]{0,39}$", var.name_prefix))
    error_message = "name_prefix must start with a lowercase letter, contain only lowercase letters, digits and hyphens, and be at most 40 characters."
  }
}

variable "cluster_name" {
  type        = string
  description = "Name of the EKS cluster running renovate-trigger, for the Pod Identity association."

  validation {
    condition     = length(var.cluster_name) > 0
    error_message = "cluster_name must not be empty."
  }
}

variable "namespace" {
  type        = string
  description = "Kubernetes namespace of the renovate-trigger release."
  default     = "renovate"

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", var.namespace))
    error_message = "namespace must be a valid RFC 1123 label."
  }
}

variable "service_account_name" {
  type        = string
  description = "Name of the ServiceAccount the pod runs as. Must match the name the chart renders — by default the release name."
  default     = "renovate-trigger"

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", var.service_account_name))
    error_message = "service_account_name must be a valid RFC 1123 label."
  }
}

variable "dependents_tag_key" {
  type        = string
  description = "AWS tag key an ECR repository carries to declare its Dependents, comma-separated. Must match the chart's config.ecr.dependentsTag."
  default     = "renovate-trigger/dependents"

  validation {
    condition     = length(var.dependents_tag_key) > 0 && length(var.dependents_tag_key) <= 128
    error_message = "dependents_tag_key must be between 1 and 128 characters."
  }
}

variable "dependency_repository_name_prefixes" {
  type        = list(string)
  description = <<-EOT
    Optional repository-name prefixes to narrow the EventBridge rule. Empty means
    every image push in the account fires, and the app cheaply drops repositories
    without the Dependents tag.

    Empty is the recommended default: a Terraform-side allowlist is the central
    allowlist ADR-0004 rejected, wearing HCL. Narrow only to keep a noisy
    high-volume CI repository out of the queue. EventBridge cannot match resource
    tags, which is why the tag read happens later, at Flush.
  EOT
  default     = []

  validation {
    condition     = alltrue([for p in var.dependency_repository_name_prefixes : length(p) > 0])
    error_message = "dependency_repository_name_prefixes must not contain empty strings."
  }

  validation {
    # An event pattern caps at 4096 bytes.
    condition     = length(var.dependency_repository_name_prefixes) <= 20
    error_message = "dependency_repository_name_prefixes must hold at most 20 prefixes."
  }
}

variable "dependency_repository_arns" {
  type        = list(string)
  description = "ECR repository ARNs the pod may read tags from. Empty means every repository in this account and region."
  default     = []

  validation {
    condition     = alltrue([for a in var.dependency_repository_arns : can(regex("^arn:aws[a-z-]*:ecr:", a))])
    error_message = "Each entry in dependency_repository_arns must be an ECR ARN."
  }
}

variable "create_iam_role" {
  type        = bool
  description = "Create the IAM role and the Pod Identity association. Set false when IAM is managed elsewhere; the queue and rule are still created."
  default     = true
}

variable "manage_pod_identity_agent" {
  type        = bool
  description = "Create the eks-pod-identity-agent EKS addon. Off by default: a cluster-wide addon is usually owned elsewhere, and Pod Identity will not work until something installs it."
  default     = false
}

variable "permissions_boundary_arn" {
  type        = string
  description = "Optional IAM permissions boundary to attach to the role."
  default     = null

  validation {
    condition     = var.permissions_boundary_arn == null || can(regex("^arn:aws[a-z-]*:iam::", var.permissions_boundary_arn))
    error_message = "permissions_boundary_arn must be an IAM ARN."
  }
}

variable "kms_key_arn" {
  type        = string
  description = <<-EOT
    ARN of an existing customer-managed KMS key to encrypt the queues with. Null
    creates one.

    A customer-managed key is not optional here. EventBridge documents that it
    "does not support using Amazon SQS queues that are encrypted with an AWS
    owned key. This includes targets, as well as Amazon SQS queues specified as
    dead-letter queues for targets" — which rules out SQS-managed SSE.
  EOT
  default     = null

  validation {
    condition     = var.kms_key_arn == null || can(regex("^arn:aws[a-z-]*:kms:", var.kms_key_arn))
    error_message = "kms_key_arn must be a KMS key ARN."
  }
}

variable "kms_key_deletion_window_in_days" {
  type        = number
  description = "Waiting period before a created KMS key is deleted."
  default     = 7

  validation {
    condition     = var.kms_key_deletion_window_in_days >= 7 && var.kms_key_deletion_window_in_days <= 30
    error_message = "kms_key_deletion_window_in_days must be between 7 and 30."
  }
}

variable "message_retention_seconds" {
  type        = number
  description = <<-EOT
    How long an undelivered image-push event is kept. Defaults to one hour, not
    SQS's 14-day maximum: the queue exists to survive a rollout, not an outage.
    renovate-trigger is lossy by design (ADR-0002) and the source CronJob
    schedule is the backstop, so a long retention would only replay a fortnight
    of stale pushes into one enormous Renovate run after a long outage.
  EOT
  default     = 3600

  validation {
    condition     = var.message_retention_seconds >= 60 && var.message_retention_seconds <= 86400
    error_message = "message_retention_seconds must be between 60 and 86400 (one day)."
  }
}

variable "visibility_timeout_seconds" {
  type        = number
  description = "How long a received message stays invisible. Handling one is parse-then-add, so this only bounds redelivery after a pod crash mid-receive."
  default     = 60

  validation {
    condition     = var.visibility_timeout_seconds >= 10 && var.visibility_timeout_seconds <= 300
    error_message = "visibility_timeout_seconds must be between 10 and 300."
  }
}

variable "max_receive_count" {
  type        = number
  description = "Receives before a message is moved to the dead-letter queue. This is what surfaces a message body the app cannot parse."
  default     = 5

  validation {
    condition     = var.max_receive_count >= 1 && var.max_receive_count <= 100
    error_message = "max_receive_count must be between 1 and 100."
  }
}

variable "dlq_message_retention_seconds" {
  type        = number
  description = "How long a dead-lettered message is kept. Long by default, unlike the main queue: the value of a poison message is being able to look at it next week."
  default     = 1209600

  validation {
    condition     = var.dlq_message_retention_seconds >= 3600 && var.dlq_message_retention_seconds <= 1209600
    error_message = "dlq_message_retention_seconds must be between 3600 and 1209600 (14 days)."
  }
}

variable "event_target_max_retry_attempts" {
  type        = number
  description = "EventBridge retries before it gives up delivering to the queue."
  default     = 3

  validation {
    condition     = var.event_target_max_retry_attempts >= 0 && var.event_target_max_retry_attempts <= 185
    error_message = "event_target_max_retry_attempts must be between 0 and 185."
  }
}

variable "event_target_max_event_age_seconds" {
  type        = number
  description = "How long EventBridge keeps retrying delivery to the queue."
  default     = 900

  validation {
    condition     = var.event_target_max_event_age_seconds >= 60 && var.event_target_max_event_age_seconds <= 86400
    error_message = "event_target_max_event_age_seconds must be between 60 and 86400."
  }
}

variable "create_alarms" {
  type        = bool
  description = "Create CloudWatch alarms for a wedged consumer and a filling dead-letter queue."
  default     = false
}

variable "alarm_sns_topic_arns" {
  type        = list(string)
  description = "SNS topics notified by the alarms. Required when create_alarms is true — an alarm with no action is decoration."
  default     = []

  validation {
    condition     = !var.create_alarms || length(var.alarm_sns_topic_arns) > 0
    error_message = "alarm_sns_topic_arns must not be empty when create_alarms is true."
  }
}

variable "queue_age_alarm_threshold_seconds" {
  type        = number
  description = "Age of the oldest queued event that means the consumer is not consuming."
  default     = 900

  validation {
    condition     = var.queue_age_alarm_threshold_seconds > 0 && var.queue_age_alarm_threshold_seconds < var.message_retention_seconds
    error_message = "queue_age_alarm_threshold_seconds must be greater than 0 and less than message_retention_seconds, or the alarm can never fire."
  }
}

variable "resource_tags" {
  type        = map(string)
  description = "Additional AWS resource tags merged into everything this module creates."
  default     = {}

  validation {
    condition     = length(var.resource_tags) <= 40
    error_message = "resource_tags must hold at most 40 entries; AWS caps a resource at 50 tags and this module sets some itself."
  }
}
