#!/usr/bin/env bash
# Tear down all AWS resources. Idempotent: safe to re-run, also after a partial deploy.
# First removes things that would block `terraform destroy`, then destroys, then verifies.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$ROOT/scripts/aws-env.sh"

log "identity: $(aws sts get-caller-identity --query Arn --output text | redact)"
mkdir -p infra/build

# 1. Stop running Step Functions executions.
for sm in $(aws stepfunctions list-state-machines --query "stateMachines[?starts_with(name,'payout-saga')].stateMachineArn" --output text); do
  for ex in $(aws stepfunctions list-executions --state-machine-arn "$sm" --status-filter RUNNING --query 'executions[].executionArn' --output text); do
    log "stopping execution $ex"; aws stepfunctions stop-execution --execution-arn "$ex" >/dev/null || true
  done
done

# 2. Scale ECS services to 0 (so no task re-registers into Cloud Map or the ALB during destroy).
if aws ecs describe-clusters --clusters payout-saga --query 'clusters[?status==`ACTIVE`].clusterName' --output text | grep -q payout-saga; then
  for svc in $(aws ecs list-services --cluster payout-saga --query 'serviceArns[]' --output text); do
    log "scaling $svc to 0"; aws ecs update-service --cluster payout-saga --service "$svc" --desired-count 0 >/dev/null || true
  done
fi

# 3. Empty the ECR repository (also force_delete in Terraform).
if aws ecr describe-repositories --repository-names payout-saga >/dev/null 2>&1; then
  ids="$(aws ecr list-images --repository-name payout-saga --query 'imageIds[]' --output json)"
  if [[ "$ids" != "[]" ]]; then aws ecr batch-delete-image --repository-name payout-saga --image-ids "$ids" >/dev/null || true; fi
fi

# 4. Terraform destroy (retries: Lambda ENIs can take a few minutes to release their subnets/security groups).
if [[ -f infra/terraform.tfstate ]]; then
  [[ -f infra/terraform.tfvars ]] || echo 'allowed_cidr = "0.0.0.0/32"' >infra/terraform.tfvars
  tf init -input=false >/dev/null
  # destroy never reads the zips' contents, but the plan still needs the files to exist
  for l in reserve fx compliance bank capture release cancelfx notify recon; do [[ -f infra/build/$l.zip ]] || : >"infra/build/$l.zip"; done
  for attempt in 1 2 3 4; do
    if tf destroy -input=false -auto-approve; then break; fi
    log "terraform destroy attempt $attempt failed, retrying in 90 s (dependency violations clear as ENIs release)"
    sleep 90
  done
else
  log "no terraform state found; nothing for terraform to destroy"
fi

log "destroy finished; run scripts/verify-destroyed.sh"
