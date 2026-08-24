# Optional and off by default.
#
# The queue-age alarm is the one that matters. renovate-trigger is lossy by design,
# so nothing else in the system complains when a trigger is dropped: a dead or
# wedged consumer is otherwise completely invisible.

resource "aws_cloudwatch_metric_alarm" "queue_age" {
  count = var.create_alarms ? 1 : 0

  alarm_name        = "${var.name_prefix}-ecr-image-push-not-consumed"
  alarm_description = "Image push events are sitting in the renovate-trigger delivery queue; the consumer may be down or wedged."

  namespace   = "AWS/SQS"
  metric_name = "ApproximateAgeOfOldestMessage"
  dimensions  = { QueueName = aws_sqs_queue.delivery.name }

  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = 2
  comparison_operator = "GreaterThanThreshold"
  threshold           = var.queue_age_alarm_threshold_seconds

  # An empty queue emits no data points, which is the healthy state.
  treat_missing_data = "notBreaching"

  alarm_actions = var.alarm_sns_topic_arns
  ok_actions    = var.alarm_sns_topic_arns
  tags          = local.resource_tags
}

# Anything here is either an event body the app could not parse or an event
# EventBridge could not deliver at all. Both want a human.
resource "aws_cloudwatch_metric_alarm" "dlq_depth" {
  count = var.create_alarms ? 1 : 0

  alarm_name        = "${var.name_prefix}-ecr-image-push-dead-lettered"
  alarm_description = "Messages have landed in the renovate-trigger dead-letter queue."

  namespace   = "AWS/SQS"
  metric_name = "ApproximateNumberOfMessagesVisible"
  dimensions  = { QueueName = aws_sqs_queue.delivery_dlq.name }

  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = 1
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0

  treat_missing_data = "notBreaching"

  alarm_actions = var.alarm_sns_topic_arns
  ok_actions    = var.alarm_sns_topic_arns
  tags          = local.resource_tags
}
