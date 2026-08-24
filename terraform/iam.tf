# The pod assumes this role through EKS Pod Identity. Unlike IRSA, the trust
# policy carries no condition — the scoping lives entirely in the association
# below, which binds the role to one namespace and one ServiceAccount.

data "aws_iam_policy_document" "trust" {
  count = var.create_iam_role ? 1 : 0

  statement {
    effect = "Allow"
    # TagSession is mandatory for Pod Identity, not optional hardening: without it
    # the pod cannot assume the role at all.
    actions = ["sts:AssumeRole", "sts:TagSession"]

    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "consumer" {
  count = var.create_iam_role ? 1 : 0

  # Long-poll the delivery queue and acknowledge what was consumed, scoped to this
  # one queue. sqs:GetQueueUrl is deliberately absent: the URL is handed to the pod
  # as an environment variable, so it never needs to look one up by name.
  #
  # sqs:ChangeMessageVisibility is also absent. Handling a message is
  # parse-then-add, so there is no long-running work whose lease needs extending,
  # and the permission would let a compromised pod hide messages indefinitely.
  statement {
    sid    = "ConsumeDeliveryQueue"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [aws_sqs_queue.delivery.arn]
  }

  # Read the Dependents tag off the pushed-to repository at Flush time — the ECR
  # analogue of reading renovate.trigger.json from a GitHub Dependency.
  #
  # ecr:ListTagsForResource supports resource-level permissions, so this is scoped
  # rather than "*". The default is every repository in this account and region,
  # because opting in should mean adding a tag, not raising a Terraform PR.
  #
  # Blast radius, stated honestly: this reads resource tags only — no manifests, no
  # layers, no pulls. What it exposes is ECR repository names and their tags.
  statement {
    sid       = "ReadDependentsTag"
    effect    = "Allow"
    actions   = ["ecr:ListTagsForResource"]
    resources = local.dependency_repository_arns
  }

  # SQS decrypts on ReceiveMessage using the caller's credentials, so a consumer of
  # an encrypted queue needs Decrypt on its key. ViaService means this role can
  # never use the key for anything but SQS.
  statement {
    sid       = "DecryptDeliveryQueueMessages"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [local.kms_key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["sqs.${data.aws_region.current.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "consumer" {
  count = var.create_iam_role ? 1 : 0

  name                 = "${var.name_prefix}-ecr-consumer"
  description          = "renovate-trigger: consume ECR image-push events and read Dependents tags"
  assume_role_policy   = data.aws_iam_policy_document.trust[0].json
  permissions_boundary = var.permissions_boundary_arn
  tags                 = local.resource_tags
}

# Inline rather than a managed policy: one role, one policy, nothing shared, and
# its lifecycle is the role's.
resource "aws_iam_role_policy" "consumer" {
  count = var.create_iam_role ? 1 : 0

  name   = "${var.name_prefix}-ecr-consumer"
  role   = aws_iam_role.consumer[0].id
  policy = data.aws_iam_policy_document.consumer[0].json
}

# Binds the role to exactly one ServiceAccount in one namespace. Renaming the Helm
# release, or overriding serviceAccount.name, means re-applying this.
resource "aws_eks_pod_identity_association" "consumer" {
  count = var.create_iam_role ? 1 : 0

  cluster_name    = var.cluster_name
  namespace       = var.namespace
  service_account = var.service_account_name
  role_arn        = aws_iam_role.consumer[0].arn
  tags            = local.resource_tags
}

# Pod Identity does nothing until the agent is running on the cluster. Off by
# default because a cluster-wide addon is usually owned by whatever provisions the
# cluster, not by this module.
resource "aws_eks_addon" "pod_identity_agent" {
  count = var.manage_pod_identity_agent ? 1 : 0

  cluster_name = var.cluster_name
  addon_name   = "eks-pod-identity-agent"
  tags         = local.resource_tags
}
