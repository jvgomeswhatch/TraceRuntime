# Phase 7 — Terraform IaC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize `infra/terraform/` from a flat `main.tf` into files by responsibility, add validate-only bootstrap, smoke test script, and full Makefile — all without recreating any infrastructure resource.

**Architecture:** Refactor-in-place — existing Terraform resource addresses and physical resource names are frozen. The `terraform plan` after reorganization must return 0 add / 0 change / 0 destroy. Bootstrap becomes validate-only (Terraform creates, bootstrap validates). Makefile becomes the single operational interface.

**Tech Stack:** Terraform ~> 1.12, AWS provider ~> 5.0, LocalStack, bash, GNU Make

---

## File Map

| Action  | File                                        | Responsibility                              |
|---------|---------------------------------------------|---------------------------------------------|
| Create  | `infra/terraform/versions.tf`               | `required_version` + `required_providers`   |
| Create  | `infra/terraform/providers.tf`              | `provider "aws"` with LocalStack endpoints  |
| Create  | `infra/terraform/sqs.tf`                    | DLQ + main queue + redrive policy           |
| Create  | `infra/terraform/s3.tf`                     | Artifact bucket                             |
| Create  | `infra/terraform/terraform.tfvars.example`  | Documented override example                 |
| Modify  | `infra/terraform/variables.tf`              | Add `aws_region` variable                   |
| Modify  | `infra/terraform/outputs.tf`                | Rename outputs + add `artifact_bucket_arn`  |
| Delete  | `infra/terraform/main.tf`                   | Replaced by the four files above            |
| Modify  | `scripts/bootstrap.sh`                      | Validate-only — no longer creates resources |
| Create  | `scripts/smoke-test.sh`                     | 11-check infrastructure validation          |
| Modify  | `Makefile`                                  | Full operational interface                  |
| Modify  | `.gitignore`                                | Remove lock file exclusion, fix tf paths    |

---

## Task 1: Create versions.tf and providers.tf

**Files:**
- Create: `infra/terraform/versions.tf`
- Create: `infra/terraform/providers.tf`

- [ ] **Step 1.1: Create versions.tf**

```hcl
# infra/terraform/versions.tf
terraform {
  required_version = "~> 1.12"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
```

Note: no `backend "local"` block — Terraform defaults to `terraform.tfstate` locally. The current `main.tf` has this block; removing it is safe and produces no plan diff.

- [ ] **Step 1.2: Create providers.tf**

```hcl
# infra/terraform/providers.tf
provider "aws" {
  region                      = var.aws_region
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
```

Note: `region` now uses `var.aws_region` instead of the hardcoded `"us-east-1"` that was in `main.tf`. The variable will be added in Task 2.

---

## Task 2: Create sqs.tf, s3.tf, and update variables.tf

**Files:**
- Create: `infra/terraform/sqs.tf`
- Create: `infra/terraform/s3.tf`
- Modify: `infra/terraform/variables.tf`

- [ ] **Step 2.1: Create sqs.tf**

Resource names (`traceruntime-tasks`, `traceruntime-tasks-dlq`) and Terraform addresses (`aws_sqs_queue.tasks`, `aws_sqs_queue.dlq`) must match exactly what is in `main.tf` — any difference will cause a plan diff.

One change from `main.tf`: DLQ `message_retention_seconds` increases from `86400` (24h) to `604800` (7 days). This is an in-place attribute update — no destroy/recreate.

```hcl
# infra/terraform/sqs.tf

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
```

- [ ] **Step 2.2: Create s3.tf**

Resource name (`traceruntime-outputs`) and Terraform address (`aws_s3_bucket.outputs`) must match `main.tf` exactly.

```hcl
# infra/terraform/s3.tf
resource "aws_s3_bucket" "outputs" {
  bucket = "traceruntime-outputs"

  # LOCAL DEVELOPMENT ONLY
  # Never use force_destroy = true in production environments.
  force_destroy = true
}
```

- [ ] **Step 2.3: Update variables.tf — add aws_region**

Current `variables.tf` only has `localstack_endpoint`. Add `aws_region`:

```hcl
# infra/terraform/variables.tf
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
```

---

## Task 3: Update outputs.tf and create terraform.tfvars.example

**Files:**
- Modify: `infra/terraform/outputs.tf`
- Create: `infra/terraform/terraform.tfvars.example`

- [ ] **Step 3.1: Update outputs.tf**

Current outputs use different names (`tasks_queue_url`, `dlq_url`, `s3_bucket`). Rename to match the spec and add `artifact_bucket_arn`. This is a rename of output names only — no resource change.

```hcl
# infra/terraform/outputs.tf
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
```

- [ ] **Step 3.2: Create terraform.tfvars.example**

```hcl
# infra/terraform/terraform.tfvars.example
# Copy to terraform.tfvars to override defaults.
# terraform.tfvars is gitignored — this file is the committed reference.

localstack_endpoint = "http://localhost:4566"
aws_region          = "us-east-1"
```

---

## Task 4: Delete main.tf and validate

**Files:**
- Delete: `infra/terraform/main.tf`

This is the critical step. All content from `main.tf` is now distributed across `versions.tf`, `providers.tf`, `sqs.tf`, `s3.tf`, `variables.tf`, and `outputs.tf`. Deleting it before running `terraform plan` will expose any missing declarations.

- [ ] **Step 4.1: Delete main.tf**

```bash
rm infra/terraform/main.tf
```

- [ ] **Step 4.2: Run terraform fmt**

```bash
cd infra/terraform && terraform fmt -recursive
```

Expected: no output (already formatted) or minor alignment fixes printed. Exit code 0.

- [ ] **Step 4.3: Run terraform init**

LocalStack must be running for this step. If not running, start it first:
```bash
docker compose --profile no-ai up -d localstack
```

Then:
```bash
cd infra/terraform && terraform init -input=false
```

Expected output includes:
```
Terraform has been successfully initialized!
```

If you see a provider download, that is normal on first init after adding `versions.tf`.

- [ ] **Step 4.4: Run terraform validate**

```bash
cd infra/terraform && terraform validate
```

Expected:
```
Success! The configuration is valid.
```

- [ ] **Step 4.5: Run terraform plan — must be zero diff**

```bash
cd infra/terraform && terraform plan
```

Expected last line:
```
No changes. Your infrastructure matches the configuration.
```

**If you see changes:** stop and investigate before proceeding. Do NOT run `terraform apply`. Check whether the diff is from:
- An attribute value change (e.g., DLQ retention 86400 → 604800) — this is expected and safe to apply
- A resource address change — this should not happen; re-check resource names in `sqs.tf` and `s3.tf`
- A destroy/recreate — this must never happen; revert and investigate

The DLQ retention change (86400 → 604800) will show as `~ update in-place`. This is the only expected diff. Apply it:

```bash
cd infra/terraform && terraform apply -auto-approve
```

Then re-run plan to confirm zero diff:
```bash
cd infra/terraform && terraform plan
```

Expected: `No changes.`

- [ ] **Step 4.6: Commit Terraform refactor**

```bash
git add infra/terraform/versions.tf infra/terraform/providers.tf \
        infra/terraform/sqs.tf infra/terraform/s3.tf \
        infra/terraform/variables.tf infra/terraform/outputs.tf \
        infra/terraform/terraform.tfvars.example
git rm infra/terraform/main.tf
git commit -m "refactor(terraform): split main.tf into files by responsibility

- versions.tf: required_version ~> 1.12, aws provider ~> 5.0
- providers.tf: LocalStack endpoints, s3_use_path_style
- sqs.tf: DLQ (7d retention) + main queue + redrive policy
- s3.tf: artifact bucket
- variables.tf: add aws_region variable
- outputs.tf: rename outputs, add artifact_bucket_arn
- terraform.tfvars.example: committed reference for overrides
- main.tf removed

terraform plan = 0 add / 0 change / 0 destroy (except DLQ retention update)"
```

---

## Task 5: Update .gitignore

**Files:**
- Modify: `.gitignore`

Current `.gitignore` has:
```
infra/terraform/.terraform/
infra/terraform/.terraform.lock.hcl   ← must be removed (lock file should be committed)
infra/terraform/terraform.tfstate
infra/terraform/terraform.tfstate.backup
```

- [ ] **Step 5.1: Update the Terraform section in .gitignore**

Replace the current Terraform block:
```
# Terraform
infra/terraform/.terraform/
infra/terraform/.terraform.lock.hcl
infra/terraform/terraform.tfstate
infra/terraform/terraform.tfstate.backup
```

With:
```
# Terraform — state and ephemeral dirs are not committed
# .terraform.lock.hcl IS committed (pins provider versions for reproducibility)
*.tfstate
*.tfstate.*
.terraform/
terraform.tfvars
```

The glob patterns (`*.tfstate`, `.terraform/`) are broader and cover any future Terraform directories without needing path-specific entries.

- [ ] **Step 5.2: Commit .terraform.lock.hcl**

The lock file now exists in `infra/terraform/` from the `terraform init` in Task 4. With `.terraform.lock.hcl` no longer gitignored, add it:

```bash
git add .gitignore infra/terraform/.terraform.lock.hcl
git commit -m "chore(terraform): commit lock file, update gitignore

- .terraform.lock.hcl committed: pins provider versions for reproducibility
- gitignore updated: use glob patterns, remove lock file exclusion
- terraform.tfvars excluded (tfvars.example is the committed reference)"
```

---

## Task 6: Rewrite bootstrap.sh as validate-only

**Files:**
- Modify: `scripts/bootstrap.sh`

Bootstrap no longer creates resources. It validates that Terraform has already provisioned them. If any resource is missing it exits 1 with a clear message directing the operator to run `make infra-apply`.

- [ ] **Step 6.1: Replace bootstrap.sh**

```bash
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
    echo "ERROR: Run 'make infra-apply' to provision infrastructure before starting the environment."
    FAILED=1
  fi
}

check_bucket() {
  local name="$1"
  printf "==> Verifying s3://%s... " "$name"
  if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
       s3api head-bucket --bucket "$name" 2>/dev/null; then
    echo "ok"
  else
    echo "MISSING"
    echo "ERROR: Run 'make infra-apply' to provision infrastructure before starting the environment."
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
```

- [ ] **Step 6.2: Make bootstrap executable and test it**

```bash
chmod +x scripts/bootstrap.sh
```

With LocalStack running and infra already provisioned, run:
```bash
./scripts/bootstrap.sh
```

Expected output:
```
==> Checking LocalStack is reachable...
    ok
==> Verifying traceruntime-tasks-dlq... ok
==> Verifying traceruntime-tasks... ok
==> Verifying s3://traceruntime-outputs... ok

Bootstrap validation complete.
```

Run it a second and third time — output must be identical. Exit code must be 0 each time.

To test the failure path, stop LocalStack and run again:
```bash
docker compose stop localstack
./scripts/bootstrap.sh
```
Expected: `ERROR: LocalStack is not reachable` with exit code 1. Restart LocalStack after this test.

- [ ] **Step 6.3: Commit bootstrap.sh**

```bash
git add scripts/bootstrap.sh
git commit -m "refactor(bootstrap): validate-only — Terraform owns resource creation

Bootstrap now verifies resources exist rather than creating them.
Fails loudly with 'make infra-apply' instruction if any resource is missing."
```

---

## Task 7: Create smoke-test.sh

**Files:**
- Create: `scripts/smoke-test.sh`

11 checks. Never exits early on failure — runs all checks and reports final count. Exit 0 = all pass, exit 1 = any failure.

- [ ] **Step 7.1: Create smoke-test.sh**

```bash
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
     s3api head-bucket --bucket traceruntime-outputs 2>/dev/null; then
  ok "traceruntime-outputs exists"
else
  fail "traceruntime-outputs exists" "bucket not found"
fi

# 4. visibility_timeout = 150
VTIMEOUT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs get-queue-attributes \
  --queue-url "$QUEUE_URL" \
  --attribute-names VisibilityTimeout \
  --query 'Attributes.VisibilityTimeout' \
  --output text 2>/dev/null)
if [ "$VTIMEOUT" = "150" ]; then
  ok "visibility_timeout = 150"
else
  fail "visibility_timeout = 150" "got ${VTIMEOUT:-<empty>}"
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
MRC=$(echo "$REDRIVE" | python -c "import sys,json; d=json.load(sys.stdin); print(d.get('maxReceiveCount',''))" 2>/dev/null || echo "")
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

# 8. Send test message
SEND_RESULT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs send-message \
  --queue-url "$QUEUE_URL" \
  --message-body '{"event_type":"smoke-test","trace_id":"smoke-test-000"}' \
  --query 'MessageId' \
  --output text 2>/dev/null)
if [ -n "$SEND_RESULT" ]; then
  ok "message sent to traceruntime-tasks (MessageId: ${SEND_RESULT})"
  MSG_ID="$SEND_RESULT"
else
  fail "message sent to traceruntime-tasks" "no MessageId returned"
  MSG_ID=""
fi

# 9. Receive test message
RECEIPT=$(aws --endpoint-url="$ENDPOINT" --region="$REGION" \
  sqs receive-message \
  --queue-url "$QUEUE_URL" \
  --max-number-of-messages 1 \
  --wait-time-seconds 2 \
  --query 'Messages[0].ReceiptHandle' \
  --output text 2>/dev/null)
if [ -n "$RECEIPT" ] && [ "$RECEIPT" != "None" ]; then
  ok "message received from traceruntime-tasks"
else
  fail "message received from traceruntime-tasks" "no message returned"
  RECEIPT=""
fi

# 10. Delete test message (cleanup)
if [ -n "$RECEIPT" ]; then
  aws --endpoint-url="$ENDPOINT" --region="$REGION" \
    sqs delete-message \
    --queue-url "$QUEUE_URL" \
    --receipt-handle "$RECEIPT" 2>/dev/null
  ok "message deleted (cleanup)"
else
  fail "message deleted (cleanup)" "no receipt handle — message was not received"
fi

# 11. S3 bucket accessible
if aws --endpoint-url="$ENDPOINT" --region="$REGION" \
     s3api list-objects-v2 --bucket traceruntime-outputs --max-items 1 \
     --output text 2>/dev/null; then
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
```

- [ ] **Step 7.2: Make executable and run**

```bash
chmod +x scripts/smoke-test.sh
./scripts/smoke-test.sh
```

Expected output (with LocalStack running and infra provisioned):
```
==> Smoke test: TraceRuntime infrastructure

✓ traceruntime-tasks exists
✓ traceruntime-tasks-dlq exists
✓ traceruntime-outputs exists
✓ visibility_timeout = 150
✓ receive_wait_time_seconds = 20
✓ maxReceiveCount = 3
✓ redrive policy configured correctly
✓ message sent to traceruntime-tasks (MessageId: <uuid>)
✓ message received from traceruntime-tasks
✓ message deleted (cleanup)
✓ S3 bucket accessible

Smoke test passed. (11/11)
```

Exit code must be 0.

- [ ] **Step 7.3: Commit smoke-test.sh**

```bash
git add scripts/smoke-test.sh
git commit -m "feat(infra): add smoke-test.sh — 11-check infrastructure validation

Validates queue existence, SQS attributes, redrive policy, message
send/receive/delete cycle, and S3 accessibility. Exit 0 = pass, 1 = fail."
```

---

## Task 8: Rewrite Makefile

**Files:**
- Modify: `Makefile`

Current Makefile has only 3 targets. Replace with full operational interface. Makefile uses tabs for indentation — do not use spaces.

- [ ] **Step 8.1: Replace Makefile**

```makefile
# ── Config ────────────────────────────────────────────────────────────────────
LOCALSTACK_ENDPOINT   ?= http://localhost:4566
HEALTH_API_URL        ?= http://localhost:8082/health
HEALTH_WORKER_METRICS ?= http://localhost:9091/metrics
HEALTH_LOCALSTACK_URL ?= http://localhost:4566/_localstack/health

# ── Terraform ─────────────────────────────────────────────────────────────────
.PHONY: infra-init infra-plan infra-apply infra-destroy infra-bootstrap infra-fmt infra-validate infra-smoke

infra-init:
	cd infra/terraform && terraform init -input=false

infra-plan: infra-init
	cd infra/terraform && terraform plan

infra-apply: infra-init
	cd infra/terraform && terraform apply -auto-approve

infra-destroy: infra-init
	cd infra/terraform && terraform destroy -auto-approve

infra-fmt:
	cd infra/terraform && terraform fmt -recursive

infra-validate: infra-init
	cd infra/terraform && terraform validate

infra-bootstrap:
	./scripts/bootstrap.sh

infra-smoke:
	./scripts/smoke-test.sh

# ── Environment ────────────────────────────────────────────────────────────────
.PHONY: up down restart reset

# First-time setup: run 'make infra-apply' before 'make up'
up:
	docker network create traceruntime 2>/dev/null || true
	docker compose -f infra/observability/docker-compose.yml up -d
	docker compose --profile no-ai up -d
	./scripts/bootstrap.sh

down:
	docker compose --profile no-ai down
	docker compose -f infra/observability/docker-compose.yml down

restart: down up

# reset tears down all state including infra — reprovisioning happens automatically
reset:
	docker compose --profile no-ai down -v
	docker compose -f infra/observability/docker-compose.yml down -v
	docker network rm traceruntime 2>/dev/null || true
	$(MAKE) infra-apply
	$(MAKE) up

# ── Operations ─────────────────────────────────────────────────────────────────
.PHONY: ps logs health queue-stats

ps:
	docker compose --profile no-ai ps
	docker compose -f infra/observability/docker-compose.yml ps

logs:
	docker compose --profile no-ai logs -f

health:
	@curl -sf $(HEALTH_API_URL) && echo " api: ok" || echo " api: FAIL"
	@curl -sf $(HEALTH_WORKER_METRICS) > /dev/null && echo " worker: ok" || echo " worker: FAIL"
	@curl -sf $(HEALTH_LOCALSTACK_URL) > /dev/null && echo " localstack: ok" || echo " localstack: FAIL"

queue-stats:
	@aws --endpoint-url=$(LOCALSTACK_ENDPOINT) --region=us-east-1 \
	  sqs get-queue-attributes \
	  --queue-url $(LOCALSTACK_ENDPOINT)/000000000000/traceruntime-tasks \
	  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed
	@aws --endpoint-url=$(LOCALSTACK_ENDPOINT) --region=us-east-1 \
	  sqs get-queue-attributes \
	  --queue-url $(LOCALSTACK_ENDPOINT)/000000000000/traceruntime-tasks-dlq \
	  --attribute-names ApproximateNumberOfMessages

# ── Development ────────────────────────────────────────────────────────────────
.PHONY: test lint fmt proto

test:
	go test ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	cd infra/terraform && terraform fmt -recursive

proto:
	buf generate
```

- [ ] **Step 8.2: Validate Makefile syntax**

```bash
make --dry-run infra-plan 2>&1 | head -5
```

Expected: shows the `terraform init` and `terraform plan` commands without executing them. No syntax errors.

```bash
make --dry-run up 2>&1 | head -10
```

Expected: shows docker network create, docker compose commands, bootstrap.sh — no errors.

- [ ] **Step 8.3: Test make ps and make health**

With services running:
```bash
make ps
```
Expected: table of running containers from both compose files.

```bash
make health
```
Expected (services running):
```
 api: ok
 worker: ok
 localstack: ok
```

- [ ] **Step 8.4: Test make queue-stats**

With LocalStack running and infra provisioned:
```bash
make queue-stats
```
Expected: JSON with `ApproximateNumberOfMessages`, `ApproximateNumberOfMessagesNotVisible`, `ApproximateNumberOfMessagesDelayed` for the main queue, and `ApproximateNumberOfMessages` for the DLQ.

- [ ] **Step 8.5: Commit Makefile**

```bash
git add Makefile
git commit -m "feat(makefile): full operational interface

Groups: Terraform, Environment, Operations, Development.
Config variables at top (LOCALSTACK_ENDPOINT, health URLs).
infra-init as shared prerequisite for terraform targets.
make up: idempotent network create + both compose stacks + bootstrap.
make reset: full teardown + infra-apply + up.
make health: curl each service health endpoint.
make queue-stats: SQS depth for main queue and DLQ."
```

---

## Task 9: Final acceptance validation

No new files. Run all acceptance criteria from the spec.

- [ ] **Step 9.1: terraform fmt — no diff**

```bash
cd infra/terraform && terraform fmt -check -recursive
```
Expected: no output, exit code 0.

- [ ] **Step 9.2: terraform validate**

```bash
cd infra/terraform && terraform validate
```
Expected: `Success! The configuration is valid.`

- [ ] **Step 9.3: terraform plan — zero diff**

```bash
cd infra/terraform && terraform plan
```
Expected: `No changes. Your infrastructure matches the configuration.`

- [ ] **Step 9.4: make infra-bootstrap — idempotent**

```bash
./scripts/bootstrap.sh && ./scripts/bootstrap.sh && ./scripts/bootstrap.sh
```
Expected: identical output all three times, exit code 0 each time.

- [ ] **Step 9.5: make infra-smoke — 11/11**

```bash
make infra-smoke
```
Expected: `Smoke test passed. (11/11)` with exit code 0.

- [ ] **Step 9.6: make up / make down cycle**

```bash
make down
make up
```
Expected: services come up cleanly, bootstrap validation passes.

- [ ] **Step 9.7: make queue-stats**

```bash
make queue-stats
```
Expected: JSON attributes returned for both queues.

- [ ] **Step 9.8: Verify gitignore**

```bash
git status infra/terraform/
```
Expected: `terraform.tfstate`, `.terraform/` do not appear as untracked.
`.terraform.lock.hcl` must appear as tracked (already committed in Task 5).

- [ ] **Step 9.9: terraform init from clean state**

```bash
rm -rf infra/terraform/.terraform
cd infra/terraform && terraform init -input=false
```
Expected: init succeeds, provider downloaded matching the lock file version.

- [ ] **Step 9.10: Final commit if any loose files**

```bash
git status
```
If anything is unstaged, add and commit with `chore(phase7): final cleanup`.
