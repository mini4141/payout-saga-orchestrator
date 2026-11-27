#!/usr/bin/env bash
# Deploy the whole stack to AWS from this PC, run the drills against it and capture the cloud proof.
#   scripts/deploy.sh            deploy + migrate + seed + drills + SFN drill
# Remember: scripts/destroy.sh right after capturing. CLAUDE.md: never leave cloud resources running.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$ROOT/scripts/aws-env.sh"

P="proof/phase2-cloud"
mkdir -p "$P/logs" "$P/tests" "$P/screenshots" .local

log "[0/8] identity check"
aws sts get-caller-identity --query Arn --output text | redact

DEPLOYER_IP="$(curl -fsS https://checkip.amazonaws.com | tr -d '[:space:]')"
export DEPLOYER_IP
cat >infra/terraform.tfvars <<EOF
allowed_cidr = "${DEPLOYER_IP}/32"
EOF

log "[1/8] build Lambda zips"
bash scripts/build-lambdas.sh >/dev/null

log "[2/8] terraform: all infrastructure with ECS services scaled to 0 (no images in ECR yet)"
tf init -input=false >/dev/null
tf apply -input=false -auto-approve -var service_count=0

REPO="$(tf_out ecr_repository_url)"
REGISTRY="${REPO%%/*}"
log "[3/8] build and push images to ECR"
aws ecr get-login-password | docker login --username AWS --password-stdin "$REGISTRY" 2>&1 | grep -v -i warning || true
docker build -t "$REPO:app" .
docker push "$REPO:app"
docker pull temporalio/temporal:1.10.1
docker tag temporalio/temporal:1.10.1 "$REPO:temporal-1.10.1"
docker push "$REPO:temporal-1.10.1"

CLUSTER="$(tf_out cluster_name)"
# A targeted apply does not refresh outputs, so look the network pieces up by name/tag.
SUBNETS="$(aws ec2 describe-subnets --filters "Name=tag:Project,Values=$PROJECT_TAG" --query 'Subnets[].SubnetId' --output text | tr '	' ',')"
SG="$(aws ec2 describe-security-groups --filters "Name=group-name,Values=payout-saga-app" "Name=tag:Project,Values=$PROJECT_TAG" --query 'SecurityGroups[0].GroupId' --output text)"
log "[4/8] run migrations + seed as a one-off Fargate task (RDS is private)"
TASK_ARN="$(aws ecs run-task --cluster "$CLUSTER" --launch-type FARGATE --task-definition payout-saga-migrate \
  --network-configuration "awsvpcConfiguration={subnets=[$SUBNETS],securityGroups=[$SG],assignPublicIp=ENABLED}" \
  --query 'tasks[0].taskArn' --output text)"
aws ecs wait tasks-stopped --cluster "$CLUSTER" --tasks "$TASK_ARN"
EXIT_CODE="$(aws ecs describe-tasks --cluster "$CLUSTER" --tasks "$TASK_ARN" --query 'tasks[0].containers[0].exitCode' --output text)"
log "migrate task exit code: $EXIT_CODE"
[[ "$EXIT_CODE" == "0" ]] || { log "migration failed"; exit 1; }

log "[5/8] terraform: start the ECS services (images are pushed and the schema exists)"
tf apply -input=false -auto-approve -var service_count=1

ALB="$(tf_out alb_dns)"
cat >.local/cloud.env <<EOF
export API_URL="http://$ALB:8080"
export MOCKS_URL_LOCAL="http://$ALB:8090"
export RECEIVER_URL_LOCAL="http://$ALB:8095"
export OPS_UI_URL="http://$ALB"
export TEMPORAL_UI_URL_LOCAL="http://$ALB:8233"
export API_TOKEN="$(tf_out api_token)"
export DEPLOYER_IP="$DEPLOYER_IP"
EOF
export TARGET=cloud
source "$ROOT/scripts/lib.sh" # re-source: picks up .local/cloud.env

log "[6/8] waiting for the services behind the ALB"
for u in "$OPS_UI_URL/healthz" "$API_URL/healthz" "$MOCKS_URL_LOCAL/healthz" "$RECEIVER_URL_LOCAL/healthz" "$TEMPORAL_UI_URL_LOCAL/"; do
  wait_http "$u" 600
done
# The worker registers with Temporal a little after its task starts.
for _ in $(seq 1 60); do
  [[ "$(psql_q "select count(*) from merchants")" == "5" ]] && break; sleep 5
done
log "services are up; merchants seeded: $(psql_q "select count(*) from merchants")"

log "[7/8] validate the ASL with the Step Functions API"
SM_ARN="$(tf_out state_machine_arn)"
aws stepfunctions describe-state-machine --state-machine-arn "$SM_ARN" --query definition --output text >.local/rendered.asl.json
aws stepfunctions validate-state-machine-definition --definition file://.local/rendered.asl.json --type STANDARD | redact >"$P/tests/asl-validate.json"
cat "$P/tests/asl-validate.json"

log "[8/8] failure drills against the cloud stack, then the Step Functions drill"
{
  bash scripts/drill-bank-reject.sh
  bash scripts/drill-bank-timeout.sh
  bash scripts/drill-worker-kill.sh
  bash scripts/drill-recon.sh
  bash scripts/check-invariants.sh
} 2>&1 | redact | tee "$P/logs/cloud-drills.log" || log "WARNING: a cloud drill reported FAIL (see $P/logs/cloud-drills.log)"
bash scripts/sfn-drill.sh 2>&1 | redact | tee "$P/logs/sfn-drill.log" || log "WARNING: the Step Functions drill reported FAIL (see $P/logs/sfn-drill.log)"
log "deploy finished. Capture proof: scripts/capture-proof.sh phase2; then scripts/destroy.sh"
