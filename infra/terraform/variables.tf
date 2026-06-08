variable "localstack_endpoint" {
  description = "LocalStack endpoint URL"
  type        = string
  default     = "http://localhost:4566"
}

variable "aws_region" {
  description = "AWS region (LocalStack)"
  type        = string
  default     = "us-east-1"
}
