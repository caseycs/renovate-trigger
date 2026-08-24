# This is not a deployable configuration. It exists so CI can `terraform validate`
# the module with every input supplied, without credentials and without a backend.
# Copy it into your own root module and adjust.

provider "aws" {
  region = "eu-west-1"
}

module "renovate_trigger_ecr" {
  source = "../.."

  cluster_name         = "platform-eu-west-1"
  namespace            = "renovate"
  service_account_name = "renovate-trigger"

  # Alarm on a wedged consumer: nothing else in renovate-trigger notices a
  # dropped trigger, so this is the only signal that ingestion has stalled.
  create_alarms        = true
  alarm_sns_topic_arns = ["arn:aws:sns:eu-west-1:123456789012:platform-alerts"]

  resource_tags = {
    "nobi.life/owner" = "platform"
  }
}

output "helm_values" {
  description = "Chart values wiring renovate-trigger to the module."
  value       = module.renovate_trigger_ecr.helm_values
}
