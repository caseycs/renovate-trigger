terraform {
  # A floor, not a pin. The repo pins exact versions where the version is its own
  # to choose (go.mod, the Dockerfile's golang tag, guarded by
  # hack/check-go-version.sh), but a module's required_version constrains every
  # consumer's root module too. Exact pins live in examples/complete and in CI,
  # where they are ours to make.
  #
  # >= 1.9 is a real requirement: the variable validation blocks below reference
  # other variables, which earlier versions reject.
  required_version = ">= 1.9.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0.0, < 7.0.0"
    }
  }
}
