output "task_queue_url" {
  value = aws_sqs_queue.tasks.url
}

output "task_dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "artifact_bucket_name" {
  value = aws_s3_bucket.outputs.bucket
}

output "artifact_bucket_arn" {
  value = aws_s3_bucket.outputs.arn
}
