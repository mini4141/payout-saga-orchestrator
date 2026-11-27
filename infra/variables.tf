variable "region" {
  type    = string
  default = "ap-southeast-2"
}

variable "aws_profile" {
  type    = string
  default = "payout-saga-orchestrator-deployer"
}

variable "allowed_cidr" {
  description = "CIDR allowed to reach the public ALB listeners (the deployer's /32, set by scripts/deploy.sh)."
  type        = string
}

variable "api_token" {
  description = "Bearer token for the Connect API (demo value)."
  type        = string
  default     = "cloud-demo-token"
}

variable "image_tag" {
  description = "Tag of the application image in ECR."
  type        = string
  default     = "app"
}

variable "temporal_image_tag" {
  type    = string
  default = "temporal-1.10.1"
}

variable "service_count" {
  description = "Desired task count per ECS service. scripts/deploy.sh applies with 0 until images are pushed and migrations have run."
  type        = number
  default     = 1
}
