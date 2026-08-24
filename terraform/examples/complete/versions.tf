# Exact pins live here, in the Dockerfile's spirit, because this directory is a
# root module whose versions are ours to choose. The module itself declares a
# floor instead, so consumers can compose it.
terraform {
  required_version = "1.14.3"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.61.0"
    }
  }
}
