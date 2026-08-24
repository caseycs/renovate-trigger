# Data sources and locals only; each resource group lives in its own file, the
# way chart/templates/ keeps one file per Kubernetes kind.

data "aws_caller_identity" "current" {}

data "aws_region" "current" {}

data "aws_partition" "current" {}

locals {
  # The key the app is told to read, echoed into an output so the chart and this
  # module cannot drift apart.
  dependents_tag_key = var.dependents_tag_key

  # Reuse the chart's label keys as AWS tag keys, so someone debugging across the
  # cluster/account boundary greps for the same strings on both sides.
  resource_tags = merge(
    {
      "app.kubernetes.io/name"       = "renovate-trigger"
      "app.kubernetes.io/managed-by" = "terraform"
    },
    var.resource_tags,
  )

  kms_key_arn = var.kms_key_arn != null ? var.kms_key_arn : aws_kms_key.delivery[0].arn

  # Default to every ECR repository in this account and region rather than "*":
  # still self-service (opting in means adding the tag, not raising a Terraform
  # PR) but bounded to one account and region.
  dependency_repository_arns = length(var.dependency_repository_arns) > 0 ? var.dependency_repository_arns : [
    "arn:${data.aws_partition.current.partition}:ecr:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:repository/*",
  ]
}
