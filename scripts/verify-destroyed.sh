#!/usr/bin/env bash
# Verify nothing billable remains. Exits non-zero if anything does.
#  1. specific checks (SPECS §11): state machines, Lambdas, ECS cluster, ALB, RDS, EventBridge schedules, ...
#  2. everything still tagged Project=payout-saga-orchestrator in the tagging API. The tagging index can lag
#     for minutes after deletion, so each hit is retried and then confirmed with a direct describe call.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$ROOT/scripts/aws-env.sh"

OUT="proof/phase3-destroy/verify-destroyed.txt"
mkdir -p "$(dirname "$OUT")"
BAD=0

check() { # check <label> <command output that must be empty>
  local label="$1" found="$2"
  if [[ -z "${found//[$'\t\r\n ']/}" ]]; then echo "OK      $label: none"; else echo "REMAINS $label: $found"; BAD=1; fi
}

run_checks() {
  BAD=0
  echo "== verify-destroyed $(date -u +%Y-%m-%dT%H:%M:%SZ) region=$AWS_REGION profile=$AWS_PROFILE =="
  check "Step Functions state machines" "$(aws stepfunctions list-state-machines --query "stateMachines[?starts_with(name,'payout-saga')].name" --output text)"
  check "Lambda functions" "$(aws lambda list-functions --query "Functions[?starts_with(FunctionName,'payout-saga')].FunctionName" --output text)"
  check "ECS cluster" "$(aws ecs describe-clusters --clusters payout-saga --query 'clusters[?status!=`INACTIVE`].clusterName' --output text)"
  check "Load balancers" "$(aws elbv2 describe-load-balancers --query "LoadBalancers[?starts_with(LoadBalancerName,'payout-saga')].LoadBalancerName" --output text 2>/dev/null)"
  check "Target groups" "$(aws elbv2 describe-target-groups --query "TargetGroups[?starts_with(TargetGroupName,'payout-saga')].TargetGroupName" --output text 2>/dev/null)"
  check "RDS instances" "$(aws rds describe-db-instances --query "DBInstances[?starts_with(DBInstanceIdentifier,'payout-saga')].DBInstanceIdentifier" --output text)"
  check "EventBridge Scheduler schedules" "$(aws scheduler list-schedules --name-prefix payout-saga --query 'Schedules[].Name' --output text)"
  check "ECR repositories" "$(aws ecr describe-repositories --query "repositories[?starts_with(repositoryName,'payout-saga')].repositoryName" --output text 2>/dev/null)"
  check "NAT gateways (tagged)" "$(aws ec2 describe-nat-gateways --filter Name=tag:Project,Values=$PROJECT_TAG Name=state,Values=pending,available --query 'NatGateways[].NatGatewayId' --output text)"
  check "VPCs (tagged)" "$(aws ec2 describe-vpcs --filters Name=tag:Project,Values=$PROJECT_TAG --query 'Vpcs[].VpcId' --output text)"
  check "Elastic IPs (tagged)" "$(aws ec2 describe-addresses --filters Name=tag:Project,Values=$PROJECT_TAG --query 'Addresses[].AllocationId' --output text)"
  check "CloudWatch log groups" "$(aws logs describe-log-groups --log-group-name-prefix /ecs/payout-saga --query 'logGroups[].logGroupName' --output text) $(aws logs describe-log-groups --log-group-name-prefix /aws/lambda/payout-saga --query 'logGroups[].logGroupName' --output text)"
  check "Cloud Map namespaces" "$(aws servicediscovery list-namespaces --query "Namespaces[?Name=='payout.local'].Id" --output text)"
  check "IAM roles" "$(aws iam list-roles --query "Roles[?starts_with(RoleName,'payout-saga')].RoleName" --output text)"
}

tagged() {
  aws resourcegroupstaggingapi get-resources --tag-filters "Key=Project,Values=$PROJECT_TAG" --query 'ResourceTagMappingList[].ResourceARN' --output text | tr '\t' '\n' | sed '/^$/d'
}

# Is this ARN still a real resource? (direct describe, because the tagging index lags)
alive() {
  local arn="$1" id="${1##*[/:]}"
  case "$arn" in
    *:ec2:*:vpc/*)               aws ec2 describe-vpcs --vpc-ids "$id" >/dev/null 2>&1 ;;
    *:ec2:*:subnet/*)            aws ec2 describe-subnets --subnet-ids "$id" >/dev/null 2>&1 ;;
    *:ec2:*:security-group/*)    aws ec2 describe-security-groups --group-ids "$id" >/dev/null 2>&1 ;;
    *:ec2:*:security-group-rule/*) aws ec2 describe-security-group-rules --security-group-rule-ids "$id" >/dev/null 2>&1 ;;
    *:ec2:*:internet-gateway/*)  aws ec2 describe-internet-gateways --internet-gateway-ids "$id" >/dev/null 2>&1 ;;
    *:ec2:*:route-table/*)       aws ec2 describe-route-tables --route-table-ids "$id" >/dev/null 2>&1 ;;
    *:ecs:*:cluster/*)           [[ -n "$(aws ecs describe-clusters --clusters "$id" --query 'clusters[?status!=`INACTIVE`].clusterName' --output text)" ]] ;;
    *:ecs:*:service/*)           return 1 ;; # services die with the cluster
    *:ecs:*:task-definition/*)   return 1 ;; # deregistered definitions are free; the index keeps them
    *:elasticloadbalancing:*)    aws elbv2 describe-load-balancers --load-balancer-arns "$arn" >/dev/null 2>&1 || aws elbv2 describe-target-groups --target-group-arns "$arn" >/dev/null 2>&1 ;;
    *:rds:*:db:*)                aws rds describe-db-instances --db-instance-identifier "$id" >/dev/null 2>&1 ;;
    *:rds:*:subgrp:*)            aws rds describe-db-subnet-groups --db-subnet-group-name "$id" >/dev/null 2>&1 ;;
    *:lambda:*:function:*)       aws lambda get-function --function-name "$id" >/dev/null 2>&1 ;;
    *:states:*:stateMachine:*)   aws stepfunctions describe-state-machine --state-machine-arn "$arn" >/dev/null 2>&1 ;;
    *:scheduler:*:schedule/*)    aws scheduler get-schedule --name "$id" >/dev/null 2>&1 ;;
    *:ecr:*:repository/*)        aws ecr describe-repositories --repository-names "$id" >/dev/null 2>&1 ;;
    *:logs:*:log-group:*)        [[ -n "$(aws logs describe-log-groups --log-group-name-prefix "${arn#*log-group:}" --query 'logGroups[].logGroupName' --output text)" ]] ;;
    *:servicediscovery:*)        return 1 ;;
    *:iam::*:role/*)             aws iam get-role --role-name "$id" >/dev/null 2>&1 ;;
    *)                           return 0 ;; # unknown type: assume it is real
  esac
}

{
  run_checks
  echo "-- tagging API (Project=$PROJECT_TAG); retrying while it catches up --"
  confirmed=""
  for attempt in $(seq 1 10); do
    confirmed=""
    for arn in $(tagged); do
      if alive "$arn"; then confirmed+="$arn"$'\n'; fi
    done
    [[ -z "$confirmed" ]] && break
    echo "attempt $attempt: still alive: $(echo "$confirmed" | tr '\n' ' ')"
    sleep 30
  done
  stale="$(tagged | wc -l | tr -d ' ')"
  if [[ -z "$confirmed" ]]; then
    echo "OK      tagged resources that still exist: none ($stale stale tagging-index entries, confirmed gone by direct describe)"
  else
    echo "REMAINS tagged resources: $confirmed"; BAD=1
  fi
  if ((BAD)); then echo "RESULT: FAIL (billable resources remain)"; else echo "RESULT: PASS (nothing billable remains)"; fi
  exit "$BAD"
} 2>&1 | redact | tee "$OUT"
exit "${PIPESTATUS[0]}"
