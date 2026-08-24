# ECR publishes to the default event bus with no opt-in, so there is no bus to
# create and nothing to enable — only a rule selecting the events we care about.

locals {
  # Three filters, each load-bearing:
  #
  #   detail-type   "ECR Image Action" is also used for DELETE and for
  #                 UPDATE_STORAGE_CLASS (archive/restore), so it alone is not
  #                 enough. Replication and referrer actions have their own
  #                 detail-types and deliberately do not fire this rule.
  #   action-type   PUSH is what makes this an image push.
  #   result        A failed push is not new content.
  #
  # Deliberately NOT filtered:
  #
  #   image-tag         absent from the event for untagged (digest-only) pushes,
  #                     so filtering on it would silently drop them — and the
  #                     concrete tag is irrelevant here, exactly as it is on the
  #                     GitHub side.
  #   resources         always [] for this detail-type.
  #   repository-name   only when dependency_repository_name_prefixes is set; a
  #                     repository opts in by carrying the Dependents tag, not by
  #                     appearing in Terraform.
  image_push_event_pattern = {
    source        = ["aws.ecr"]
    "detail-type" = ["ECR Image Action"]
    detail = merge(
      {
        "action-type" = ["PUSH"]
        result        = ["SUCCESS"]
      },
      length(var.dependency_repository_name_prefixes) > 0 ? {
        "repository-name" = [for p in var.dependency_repository_name_prefixes : { prefix = p }]
      } : {},
    )
  }
}

resource "aws_cloudwatch_event_rule" "image_push" {
  name        = "${var.name_prefix}-ecr-image-push"
  description = "ECR image pushes to the renovate-trigger delivery queue"
  state       = "ENABLED"

  # jsonencode rather than a heredoc: it emits keys in a stable order, so the
  # value matches what the API returns and there is no perpetual diff.
  event_pattern = jsonencode(local.image_push_event_pattern)

  tags = local.resource_tags
}

resource "aws_cloudwatch_event_target" "delivery" {
  rule = aws_cloudwatch_event_rule.image_push.name
  arn  = aws_sqs_queue.delivery.arn

  # No input transformer: the pod parses the whole envelope, so a field ECR adds
  # later is additive rather than a schema break here.

  retry_policy {
    maximum_event_age_in_seconds = var.event_target_max_event_age_seconds
    maximum_retry_attempts       = var.event_target_max_retry_attempts
  }

  # Catches the failure this design is most likely to hit: EventBridge could not
  # enqueue at all, because the queue policy or the KMS key policy is wrong. That
  # is a different failure from a message the pod could not parse, which the
  # queue's own redrive policy routes to the same dead-letter queue.
  dead_letter_config {
    arn = aws_sqs_queue.delivery_dlq.arn
  }

  # Ordering, not data dependency: without this the target can exist before the
  # queue policy does, and pushes in that window are dropped with no trace.
  depends_on = [
    aws_sqs_queue_policy.delivery,
    aws_sqs_queue_policy.delivery_dlq,
  ]
}
