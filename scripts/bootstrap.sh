#!/usr/bin/env bash
# bootstrap.sh — validate that Terraform-provisioned infrastructure exists.
# Does NOT create resources. Run 'make infra-apply' first.
set -euo pipefail

ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"

FAILED=0

check_queue() {
  local name="$1"
  printf "==> Verifying %s... " "$name"
  if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
       sqs get-queue-url --queue-name "$name" \
       --output text 2>/dev/null | grep -q "$name"; then
    echo "ok"
  else
    echo "MISSING"
    echo "ERROR: Run 'make bootstrap' to provision infrastructure before starting the environment."
    FAILED=1
  fi
}

check_bucket() {
  local name="$1"
  printf "==> Verifying s3://%s... " "$name"
  if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
       s3api head-bucket --bucket "$name" > /dev/null 2>&1; then
    echo "ok"
  else
    echo "MISSING"
    echo "ERROR: Run 'make bootstrap' to provision infrastructure before starting the environment."
    FAILED=1
  fi
}

echo "==> Checking LocalStack is reachable..."
if ! curl -sf "${ENDPOINT}/_localstack/health" > /dev/null 2>&1; then
  echo "ERROR: LocalStack is not reachable at ${ENDPOINT}"
  echo "Start it with: docker compose --profile no-ai up -d localstack"
  exit 1
fi
echo "    ok"

check_queue "traceruntime-tasks-dlq"
check_queue "traceruntime-tasks"
check_bucket "traceruntime-outputs"

if [ "$FAILED" -eq 1 ]; then
  exit 1
fi

echo ""
echo "Bootstrap validation complete."
