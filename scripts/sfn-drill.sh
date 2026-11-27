#!/usr/bin/env bash
# Step Functions drill (cloud): five executions with different injected failures. Saves each
# execution history, a summary (sfn-executions.json) and the compensation history.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$ROOT/scripts/aws-env.sh"
export TARGET=cloud
source "$ROOT/scripts/lib.sh"

P="proof/phase2-cloud/logs"
mkdir -p "$P"
SM_ARN="$(tf_out state_machine_arn)"
log "== Step Functions drill on $(basename "$SM_ARN") =="

# name | service | injected config | expected final status
scenarios=(
  "happy-path|bank|{}|SUCCEEDED"
  "bank-rejected|bank|{\"reject_next\":1}|COMPENSATED"
  "bank-timeout-retry|bank|{\"timeout_next\":2}|SUCCEEDED"
  "compliance-blocked|compliance|{\"reject_next\":1}|COMPENSATED"
  "fx-errors-retry|fx|{\"error_next\":2}|SUCCEEDED"
)
merchants=(m_acme m_bluebird m_cobalt m_delta m_ember)
summary="[]"
stamp="$(date +%s)"
i=0
for sc in "${scenarios[@]}"; do
  IFS='|' read -r name svc cfg want <<<"$sc"
  mock_reset
  [[ "$cfg" == "{}" ]] || mock_cfg "$svc" "$cfg"
  ENGINE=sfn pid="$(ENGINE=sfn create_payout "${merchants[$((i % 5))]}" $((15000 + 500 * i)))"
  exec_arn="$(aws stepfunctions start-execution --state-machine-arn "$SM_ARN" --name "drill-$name-$stamp" \
    --input "{\"payout_id\":\"$pid\"}" --query executionArn --output text)"
  log "[$name] payout=$pid execution started (inject $svc $cfg)"
  status=RUNNING
  for _ in $(seq 1 90); do
    status="$(aws stepfunctions describe-execution --execution-arn "$exec_arn" --query status --output text)"
    [[ "$status" != "RUNNING" ]] && break
    sleep 2
  done
  aws stepfunctions get-execution-history --execution-arn "$exec_arn" --output json | redact >"$P/sfn-history-$name.json"
  final="$(payout_status "$pid")"
  states="$(jq -c '[.events[] | select(.type=="TaskStateEntered") | .stateEnteredEventDetails.name]' "$P/sfn-history-$name.json")"
  retries="$(jq '[.events[] | select(.type=="TaskFailed")] | length' "$P/sfn-history-$name.json")"
  log "[$name] execution=$status payout=$final retried/failed task events=$retries states=$states"
  expect "[$name] payout status" "$final" "$want"
  summary="$(jq -c --arg n "$name" --arg p "$pid" --arg s "$status" --arg f "$final" --arg w "$want" --argjson st "$states" --argjson r "$retries" \
    '. + [{scenario:$n, payout_id:$p, execution_status:$s, payout_status:$f, expected:$w, task_failures:$r, states_entered:$st}]' <<<"$summary")"
  i=$((i + 1))
done
mock_reset

echo "$summary" | jq '.' | redact >"$P/sfn-executions.json"
cp "$P/sfn-history-bank-rejected.json" "$P/sfn-history-compensation.json"
log "compensation path in the bank-rejected execution (Catch → compensation states):"
jq -r '[.events[] | select(.type=="TaskStateEntered") | .stateEnteredEventDetails.name] | join(" → ")' "$P/sfn-history-bank-rejected.json"
log "retry evidence in the bank-timeout execution (TaskFailed events, then success):"
jq -r '.events[] | select(.type=="TaskFailed" or .type=="TaskSucceeded") | "\(.id) \(.type) \(.taskFailedEventDetails.error // "")"' "$P/sfn-history-bank-timeout-retry.json" | head -12
finish
