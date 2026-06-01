#!/usr/bin/env bash
set -euo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ACCOUNT="000000000000"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"

echo "==> Creating DLQ..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs create-queue \
  --queue-name traceruntime-tasks-dlq \
  --attributes '{"MessageRetentionPeriod":"86400"}' \
  --output json

DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT}:traceruntime-tasks-dlq"

echo "==> Creating main task queue..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs create-queue \
  --queue-name traceruntime-tasks \
  --attributes "{
    \"VisibilityTimeout\": \"150\",
    \"MessageRetentionPeriod\": \"86400\",
    \"ReceiveMessageWaitTimeSeconds\": \"20\",
    \"RedrivePolicy\": \"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"3\\\"}\"
  }" \
  --output json

echo "==> Creating S3 bucket..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" s3api create-bucket \
  --bucket traceruntime-outputs \
  --output json

echo "==> Verifying queues..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs list-queues --output json

echo "==> Verifying buckets..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" s3api list-buckets --output json

echo ""
echo "Bootstrap complete."
echo "  Main queue : http://${ENDPOINT#http://}/${ACCOUNT}/traceruntime-tasks"
echo "  DLQ        : http://${ENDPOINT#http://}/${ACCOUNT}/traceruntime-tasks-dlq"
echo "  S3 bucket  : traceruntime-outputs"
