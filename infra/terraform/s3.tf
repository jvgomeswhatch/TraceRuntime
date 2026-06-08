resource "aws_s3_bucket" "outputs" {
  bucket = "traceruntime-outputs"

  # LOCAL DEVELOPMENT ONLY
  # Never use force_destroy = true in production environments.
  force_destroy = true
}
