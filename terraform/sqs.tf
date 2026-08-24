# The delivery queue carries Image push events from the EventBridge rule to the
# pod. It is transport, not durability: the app deletes each message as soon as the
# Dependency joins the Batch, and retention is deliberately short.

resource "aws_sqs_queue" "delivery" {
  name = "${var.name_prefix}-ecr-image-push"

  # 20 seconds is SQS's maximum long-poll wait and the whole point of the design:
  # one consumer polling at 20s makes roughly 4,300 receive calls a day, which
  # stays inside the free tier. A lower value is a cost bug, not a latency win, so
  # it is not exposed as a variable.
  receive_wait_time_seconds = 20

  visibility_timeout_seconds = var.visibility_timeout_seconds
  message_retention_seconds  = var.message_retention_seconds

  kms_master_key_id                 = local.kms_key_arn
  kms_data_key_reuse_period_seconds = 300

  # A malformed body is deliberately left on the queue by the app rather than
  # deleted, so this redrive policy is what surfaces it instead of hiding it.
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.delivery_dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = local.resource_tags
}

resource "aws_sqs_queue" "delivery_dlq" {
  name = "${var.name_prefix}-ecr-image-push-dlq"

  message_retention_seconds = var.dlq_message_retention_seconds
  kms_master_key_id         = local.kms_key_arn

  tags = local.resource_tags
}

resource "aws_sqs_queue_redrive_allow_policy" "delivery_dlq" {
  queue_url = aws_sqs_queue.delivery_dlq.url

  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.delivery.arn]
  })
}

data "aws_iam_policy_document" "delivery_queue" {
  # Only the one rule this module created may enqueue. SourceArn pins it to that
  # rule rather than to any EventBridge rule; SourceAccount closes the
  # confused-deputy hole where a rule in another account names our queue as its
  # target.
  statement {
    sid       = "AllowImagePushRuleToEnqueue"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.delivery.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.image_push.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }

  # SQS has no transport-level default deny, so without this a signed request
  # over plain HTTP is accepted.
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.delivery.arn]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_sqs_queue_policy" "delivery" {
  queue_url = aws_sqs_queue.delivery.url
  policy    = data.aws_iam_policy_document.delivery_queue.json
}

# The dead-letter queue needs its own grant: it is named as the event target's
# dead-letter config, so EventBridge writes to it directly when it cannot deliver
# to the main queue at all. Omitting this is the step that makes undeliverable
# events vanish instead of landing somewhere visible.
data "aws_iam_policy_document" "delivery_dlq" {
  statement {
    sid       = "AllowImagePushRuleToEnqueueUndeliverable"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.delivery_dlq.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.image_push.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }

  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.delivery_dlq.arn]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_sqs_queue_policy" "delivery_dlq" {
  queue_url = aws_sqs_queue.delivery_dlq.url
  policy    = data.aws_iam_policy_document.delivery_dlq.json
}
