output "delivery_queue_url" {
  description = "SQS queue URL to set as the chart's config.ecr.queueUrl."
  value       = aws_sqs_queue.delivery.url
}

output "delivery_queue_arn" {
  description = "ARN of the delivery queue."
  value       = aws_sqs_queue.delivery.arn
}

output "delivery_queue_name" {
  description = "Name of the delivery queue, for CloudWatch dimensions."
  value       = aws_sqs_queue.delivery.name
}

output "dead_letter_queue_url" {
  description = "URL of the dead-letter queue holding unparseable and undeliverable events."
  value       = aws_sqs_queue.delivery_dlq.url
}

output "dead_letter_queue_arn" {
  description = "ARN of the dead-letter queue."
  value       = aws_sqs_queue.delivery_dlq.arn
}

output "role_arn" {
  description = "ARN of the role the pod assumes via Pod Identity. Null when create_iam_role is false."
  value       = var.create_iam_role ? aws_iam_role.consumer[0].arn : null
}

output "role_name" {
  description = "Name of the consumer role. Null when create_iam_role is false."
  value       = var.create_iam_role ? aws_iam_role.consumer[0].name : null
}

output "event_rule_arn" {
  description = "ARN of the EventBridge rule selecting ECR image pushes."
  value       = aws_cloudwatch_event_rule.image_push.arn
}

output "kms_key_arn" {
  description = "ARN of the KMS key encrypting the queues."
  value       = local.kms_key_arn
}

output "region" {
  description = "AWS region to set as the chart's config.ecr.region."
  value       = data.aws_region.current.region
}

output "dependents_tag_key" {
  description = "The Dependents tag key, echoed so the chart and this module cannot drift."
  value       = local.dependents_tag_key
}

output "helm_values" {
  description = <<-EOT
    YAML fragment wiring the Helm chart to this module. Write it out and pass it
    alongside your own values file:

      terraform output -raw helm_values > renovate-trigger.aws.values.yaml
      helm upgrade renovate-trigger ... -f renovate-trigger.values.yaml -f renovate-trigger.aws.values.yaml
  EOT
  value = yamlencode({
    config = {
      ecr = {
        enabled       = true
        queueUrl      = aws_sqs_queue.delivery.url
        region        = data.aws_region.current.region
        dependentsTag = local.dependents_tag_key
      }
    }
  })
}
