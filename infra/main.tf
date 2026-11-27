terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.80, < 7.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
  # State stays local and gitignored (see .gitignore).
}

provider "aws" {
  region  = var.region
  profile = var.aws_profile

  default_tags {
    tags = {
      Project   = "payout-saga-orchestrator"
      Ephemeral = "true"
      ManagedBy = "terraform"
      Owner     = "alvieirvansyah"
    }
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  name = "payout-saga"
  azs  = slice(data.aws_availability_zones.available.names, 0, 2)
}
