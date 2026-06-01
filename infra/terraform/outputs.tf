output "tasks_queue_url" {
  value = aws_sqs_queue.tasks.url
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "s3_bucket" {
  value = aws_s3_bucket.outputs.bucket
}
