# DLQ — 7 days retention for operational inspection window (weekends, incidents)
resource "aws_sqs_queue" "dlq" {
  name                      = "traceruntime-tasks-dlq"
  message_retention_seconds = 604800
}

resource "aws_sqs_queue" "tasks" {
  name = "traceruntime-tasks"

  # Calibrated from Phase 9A loadtest (n=10): processing p95 = 326s.
  # 360s provides margin for p99 variance under sustained load.
  visibility_timeout_seconds = 360

  message_retention_seconds = 86400
  receive_wait_time_seconds = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}
