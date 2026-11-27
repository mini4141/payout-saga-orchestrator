#!/usr/bin/env bash
# Capture Phase 2 proof from the deployed stack: resource list, cloud e2e test run, screenshots,
# CloudWatch graph and logs. Run after scripts/deploy.sh and before scripts/destroy.sh.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$ROOT/scripts/aws-env.sh"
export TARGET=cloud
source "$ROOT/scripts/lib.sh"

P="proof/phase2-cloud"
mkdir -p "$P/logs" "$P/tests" "$P/screenshots"

log "resources.txt"
{
  echo "# Deployed AWS resources ($(date -u +%Y-%m-%dT%H:%M:%SZ)), region $AWS_REGION"
  echo; echo "## terraform outputs"; tf output | grep -v api_token
  echo; echo "## ECS services"
  aws ecs describe-services --cluster payout-saga --services api worker relay mocks receiver ops-ui temporal \
    --query 'services[].{service:serviceName,desired:desiredCount,running:runningCount,launchType:launchType,taskDefinition:taskDefinition}' --output table
  echo; echo "## Load balancer"
  aws elbv2 describe-load-balancers --names payout-saga --query 'LoadBalancers[].{name:LoadBalancerName,scheme:Scheme,type:Type,state:State.Code}' --output table
  aws elbv2 describe-listeners --load-balancer-arn "$(aws elbv2 describe-load-balancers --names payout-saga --query 'LoadBalancers[0].LoadBalancerArn' --output text)" --query 'Listeners[].{port:Port,protocol:Protocol}' --output table
  echo; echo "## RDS"
  aws rds describe-db-instances --db-instance-identifier payout-saga \
    --query 'DBInstances[].{id:DBInstanceIdentifier,class:DBInstanceClass,engine:Engine,version:EngineVersion,status:DBInstanceStatus,public:PubliclyAccessible,multiAZ:MultiAZ}' --output table
  echo; echo "## Lambda functions"
  aws lambda list-functions --query "Functions[?starts_with(FunctionName,'payout-saga')].{name:FunctionName,runtime:Runtime,arch:Architectures[0],memory:MemorySize}" --output table
  echo; echo "## Step Functions"
  aws stepfunctions list-state-machines --query "stateMachines[?starts_with(name,'payout-saga')].{name:name,type:type}" --output table
  echo; echo "## EventBridge Scheduler"
  aws scheduler get-schedule --name payout-saga-recon --query '{name:Name,expression:ScheduleExpression,state:State}' --output table
  echo; echo "## ECR"
  aws ecr describe-images --repository-name payout-saga --query 'imageDetails[].{tags:imageTags[0],sizeMB:imageSizeInBytes}' --output table
  echo; echo "## Tagged resources (Project=$PROJECT_TAG)"
  aws resourcegroupstaggingapi get-resources --tag-filters "Key=Project,Values=$PROJECT_TAG" --query 'ResourceTagMappingList[].ResourceARN' --output text | tr '\t' '\n' | sort
} 2>&1 | redact >"$P/resources.txt"

log "EventBridge-triggered reconciliation Lambda invocations"
aws logs filter-log-events --log-group-name /aws/lambda/payout-saga-recon --query 'events[].message' --output text 2>&1 | tr '\t' '\n' | redact >"$P/logs/eventbridge-recon.log" || true

log "ECS service log sample"
aws logs tail /ecs/payout-saga --since 40m --format short 2>&1 | grep -E 'step|event|reconciliation|worker started' | tail -150 | redact >"$P/logs/ecs-service-sample.log" || true

log "cloud e2e smoke test through Connect"
E2E_API_URL="$API_URL" API_TOKEN="$API_TOKEN" go test -count=1 -v ./tests/e2e 2>&1 | tee "$P/tests/e2e-cloud.log" | go-junit-report -out "$P/tests/e2e-cloud-junit.xml" -iocopy | tail -5

log "CloudWatch graph (Lambda invocations and Step Functions executions)"
SM_ARN="$(tf_out state_machine_arn)"
# Labels keep the state machine ARN (which contains the account ID) out of the rendered PNG legend.
WIDGET=$(jq -nc --arg sm "$SM_ARN" '{
  title: "Payout saga on AWS: Step Functions executions and Lambda invocations", width: 900, height: 400, view: "timeSeries", stat: "Sum", period: 60, region: "ap-southeast-2",
  start: "-PT2H", end: "PT0H",
  metrics: [
    ["AWS/States","ExecutionsStarted","StateMachineArn",$sm,{label:"Step Functions executions started"}],
    ["AWS/States","ExecutionsSucceeded","StateMachineArn",$sm,{label:"Step Functions executions succeeded"}],
    ["AWS/Lambda","Invocations","FunctionName","payout-saga-bank",{label:"bank Lambda invocations"}],
    ["AWS/Lambda","Errors","FunctionName","payout-saga-bank",{label:"bank Lambda errors (injected timeouts)"}],
    ["AWS/Lambda","Invocations","FunctionName","payout-saga-recon",{label:"recon Lambda invocations (EventBridge Scheduler)"}]
  ]}')
aws cloudwatch get-metric-widget-image --metric-widget "$WIDGET" --output text --query MetricWidgetImage | tr -d "[:space:]" | base64 -d >"$P/screenshots/04-cloudwatch-sfn-lambda.png" || true

log "screenshots of the cloud ops UI"
retry="$(psql_q "select payout_id from step_attempts where step='SendBankTransfer' and outcome='error' group by payout_id having count(*)>=2 order by max(id) desc limit 1")"
node scripts/screenshots.mjs "$P/screenshots" "$OPS_UI_URL" "$TEMPORAL_UI_URL_LOCAL" "$retry" "" cloud
log "phase2 proof captured in $P (redacted)"
