#!/usr/bin/env bash
# Pins every AWS call in these scripts to the project profile and region. Source, do not run.
export AWS_PROFILE="payout-saga-orchestrator-deployer"
export AWS_REGION="ap-southeast-2"
export AWS_DEFAULT_REGION="ap-southeast-2"
export AWS_PAGER=""
PROJECT_TAG="payout-saga-orchestrator"

# redact masks account IDs, access keys and the deployer's public IP in text read from stdin.
redact() {
  local ip="${DEPLOYER_IP:-}"
  sed -E 's/[0-9]{12}/<ACCOUNT_ID>/g; s/(AKIA|ASIA)[A-Z0-9]{16}/<ACCESS_KEY>/g' | { if [[ -n "$ip" ]]; then sed "s/$ip/<DEPLOYER_IP>/g"; else cat; fi; }
}

tf() { terraform -chdir=infra "$@"; }
tf_out() { tf output -raw "$1"; }
