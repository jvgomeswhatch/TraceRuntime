#!/usr/bin/env bash
# smoke-test.sh — validate TraceRuntime infrastructure configuration and behavior.
# Sends one test message and deletes it (no permanent state change).
set -uo pipefail

ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ACCOUNT="000000000000"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"

PASS=0
FAIL=0

ok()   { echo "✓ $1"; PASS=$((PASS + 1)); }
fail() { echo "✗ $1 — $2"; FAIL=$((FAIL + 1)); }

QUEUE_URL="${ENDPOINT}/${ACCOUNT}/traceruntime-tasks"
DLQ_URL="${ENDPOINT}/${ACCOUNT}/traceruntime-tasks-dlq"

echo ""
echo "==> Smoke test: TraceRuntime infrastructure"
echo ""

# 1. Main queue exists
if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
     sqs get-queue-url --queue-name traceruntime-tasks \
     --output text 2>/dev/null | grep -q "traceruntime-tasks"; then
  ok "traceruntime-tasks exists"
else
  fail "traceruntime-tasks exists" "queue not found"
fi

# 2. DLQ exists
if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
     sqs get-queue-url --queue-name traceruntime-tasks-dlq \
     --output text 2>/dev/null | grep -q "traceruntime-tasks-dlq"; then
  ok "traceruntime-tasks-dlq exists"
else
  fail "traceruntime-tasks-dlq exists" "queue not found"
fi

# 3. S3 bucket exists
if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
     s3api head-bucket --bucket traceruntime-outputs > /dev/null 2>&1; then
  ok "traceruntime-outputs exists"
else
  fail "traceruntime-outputs exists" "bucket not found"
fi

# 4. visibility_timeout = 360
VTIMEOUT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs get-queue-attributes \
  --queue-url "$QUEUE_URL" \
  --attribute-names VisibilityTimeout \
  --query 'Attributes.VisibilityTimeout' \
  --output text 2>/dev/null)
if [ "$VTIMEOUT" = "360" ]; then
  ok "visibility_timeout = 360"
else
  fail "visibility_timeout = 360" "got ${VTIMEOUT:-<empty>}"
fi

# 5. receive_wait_time_seconds = 20
RWTS=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs get-queue-attributes \
  --queue-url "$QUEUE_URL" \
  --attribute-names ReceiveMessageWaitTimeSeconds \
  --query 'Attributes.ReceiveMessageWaitTimeSeconds' \
  --output text 2>/dev/null)
if [ "$RWTS" = "20" ]; then
  ok "receive_wait_time_seconds = 20"
else
  fail "receive_wait_time_seconds = 20" "got ${RWTS:-<empty>}"
fi

# 6. maxReceiveCount = 3
REDRIVE=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs get-queue-attributes \
  --queue-url "$QUEUE_URL" \
  --attribute-names RedrivePolicy \
  --query 'Attributes.RedrivePolicy' \
  --output text 2>/dev/null)
MRC=$(echo "$REDRIVE" | python -c "import sys,json; d=json.load(sys.stdin); print(str(d.get('maxReceiveCount','')))" 2>/dev/null || echo "")
if [ "$MRC" = "3" ]; then
  ok "maxReceiveCount = 3"
else
  fail "maxReceiveCount = 3" "got '${MRC}' from RedrivePolicy"
fi

# 7. redrive policy points to correct DLQ ARN
DLQ_ARN_EXPECTED="arn:aws:sqs:${REGION}:${ACCOUNT}:traceruntime-tasks-dlq"
DLQ_ARN_ACTUAL=$(echo "$REDRIVE" | python -c "import sys,json; d=json.load(sys.stdin); print(d.get('deadLetterTargetArn',''))" 2>/dev/null || echo "")
if [ "$DLQ_ARN_ACTUAL" = "$DLQ_ARN_EXPECTED" ]; then
  ok "redrive policy configured correctly"
else
  fail "redrive policy configured correctly" "expected ${DLQ_ARN_EXPECTED}, got ${DLQ_ARN_ACTUAL}"
fi

# 8. Send test message to DLQ (worker does not consume DLQ — avoids race condition)
SEND_RESULT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs send-message \
  --queue-url "$DLQ_URL" \
  --message-body '{"event_type":"smoke-test","trace_id":"smoke-test-000"}' \
  --query 'MessageId' \
  --output text 2>/dev/null)
if [ -n "$SEND_RESULT" ]; then
  ok "message sent to traceruntime-tasks-dlq (MessageId: ${SEND_RESULT})"
  MSG_ID="$SEND_RESULT"
else
  fail "message sent to traceruntime-tasks-dlq" "no MessageId returned"
  MSG_ID=""
fi

# 9. Receive test message from DLQ
RECEIPT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs receive-message \
  --queue-url "$DLQ_URL" \
  --max-number-of-messages 1 \
  --wait-time-seconds 2 \
  --query 'Messages[0].ReceiptHandle' \
  --output text 2>/dev/null)
if [ -n "$RECEIPT" ] && [ "$RECEIPT" != "None" ]; then
  ok "message received from traceruntime-tasks-dlq"
else
  fail "message received from traceruntime-tasks-dlq" "no message returned"
  RECEIPT=""
fi

# 10. Delete test message (cleanup)
if [ -n "$RECEIPT" ]; then
  aws --endpoint-url="$ENDPOINT" --region="$REGION" \
    sqs delete-message \
    --queue-url "$DLQ_URL" \
    --receipt-handle "$RECEIPT" 2>/dev/null
  ok "message deleted from DLQ (cleanup)"
else
  fail "message deleted from DLQ (cleanup)" "no receipt handle — message was not received"
fi

# 11. S3 bucket accessible
if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
     s3api list-objects-v2 --bucket traceruntime-outputs --max-items 1 \
     --output text > /dev/null 2>&1; then
  ok "S3 bucket accessible"
else
  fail "S3 bucket accessible" "list-objects-v2 failed"
fi

# Summary
TOTAL=$((PASS + FAIL))
echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "Smoke test passed. (${PASS}/${TOTAL})"
  exit 0
else
  echo "Smoke test FAILED. (${PASS}/${TOTAL})"
  exit 1
fi
