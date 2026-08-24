# A customer-managed key is required, not preferred. The EventBridge documentation
# states: "EventBridge does not support using Amazon SQS queues that are encrypted
# with an AWS owned key. This includes targets, as well as Amazon SQS queues
# specified as dead-letter queues for targets." SQS-managed SSE uses an AWS owned
# key, so it would leave the rule silently unable to enqueue.
#
# Cost: about $1/month for the key. kms_data_key_reuse_period_seconds on the queue
# keeps the per-request charges negligible.

data "aws_iam_policy_document" "delivery_key" {
  count = var.kms_key_arn == null ? 1 : 0

  # Without a root statement the key is unmanageable and unrecoverable. It also
  # delegates to IAM, which is what lets the pod's own policy grant kms:Decrypt
  # rather than duplicating that grant here.
  statement {
    sid       = "EnableAccountKeyAdministration"
    effect    = "Allow"
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }

  # EventBridge envelope-encrypts each event before SendMessage, so it needs to
  # mint a data key and to unwrap the cached one. ViaService pins the grant to
  # SQS; SourceArn pins it to this module's one rule.
  statement {
    sid       = "AllowImagePushRuleToEncrypt"
    effect    = "Allow"
    actions   = ["kms:GenerateDataKey", "kms:GenerateDataKey*", "kms:Decrypt"]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["sqs.${data.aws_region.current.region}.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.image_push.arn]
    }
  }
}

resource "aws_kms_key" "delivery" {
  count = var.kms_key_arn == null ? 1 : 0

  description             = "${var.name_prefix} ECR image-push delivery queue"
  enable_key_rotation     = true
  deletion_window_in_days = var.kms_key_deletion_window_in_days
  policy                  = data.aws_iam_policy_document.delivery_key[0].json
  tags                    = local.resource_tags
}

resource "aws_kms_alias" "delivery" {
  count = var.kms_key_arn == null ? 1 : 0

  name          = "alias/${var.name_prefix}-ecr-image-push"
  target_key_id = aws_kms_key.delivery[0].key_id
}
