# DLQ — 7 days retention for operational inspection window (weekends, incidents)
resource "aws_sqs_queue" "dlq" {
  name                      = "traceruntime-tasks-dlq"
  message_retention_seconds = 604800
}

resource "aws_sqs_queue" "tasks" {
  name = "traceruntime-tasks"

  # Provisional value.
  # Must be recalibrated in Phase 8.5 using real Ollama inference p95.
  visibility_timeout_seconds = 150

  message_retention_seconds = 86400
  receive_wait_time_seconds = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}
