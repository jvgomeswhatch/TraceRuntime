terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "local" {
    path = "terraform.tfstate"
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    sqs = var.localstack_endpoint
    s3  = var.localstack_endpoint
  }

  s3_use_path_style = true
}

# ── DLQ ──────────────────────────────────────────────────────────────────────

resource "aws_sqs_queue" "dlq" {
  name                      = "traceruntime-tasks-dlq"
  message_retention_seconds = 86400
}

# ── Main queue ────────────────────────────────────────────────────────────────

resource "aws_sqs_queue" "tasks" {
  name                       = "traceruntime-tasks"
  visibility_timeout_seconds = 150
  message_retention_seconds  = 86400
  receive_wait_time_seconds  = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}

# ── S3 bucket ─────────────────────────────────────────────────────────────────

resource "aws_s3_bucket" "outputs" {
  bucket        = "traceruntime-outputs"
  force_destroy = true
}
