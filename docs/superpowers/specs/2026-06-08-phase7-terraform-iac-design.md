# Phase 7 — Terraform IaC: Design Spec

**Date:** 2026-06-08
**Status:** Approved

---

## Objective

Consolidate and mature the Terraform infrastructure for TraceRuntime. No new AWS services are introduced. No Go or Python services are modified. The goal is reproducible, well-organized IaC that can sustain Phases 8, 8.5, and 9 without generating technical debt.

SNS was evaluated and deliberately deferred: there is currently no fan-out or multi-consumer scenario that justifies it architecturally. When a second real consumer emerges (audit, analytics, notifications), SNS will be introduced as a justified architectural evolution.

---

## Scope

- Reorganize `infra/terraform/` from a flat `main.tf` into files by responsibility
- Pin Terraform and provider versions (same major as currently in use — no provider upgrade)
- Preserve existing resource names and Terraform addresses — `terraform plan` must be a no-op
- Codify SQS operational parameters explicitly in `sqs.tf`
- Add `terraform.tfvars.example`
- Refactor `scripts/bootstrap.sh` to validate-only (Terraform creates, bootstrap validates)
- Add `scripts/smoke-test.sh` for infrastructure validation
- Expand `Makefile` with operational and development targets
- Commit `.terraform.lock.hcl` — update `.gitignore` accordingly

**Out of scope:** SNS, modules, multiple environments, CI validation (belongs to CI/CD phase), TFLint, Checkov, Terratest, resource renaming.

---

## Approach

Refactor-in-place. No `terraform destroy` or rebuild. No resource renaming.

Protocol:
1. Reorganize files — split `main.tf` into `versions.tf`, `providers.tf`, `sqs.tf`, `s3.tf`
2. `terraform fmt`
3. `terraform validate`
4. `terraform plan` — must return **0 add / 0 change / 0 destroy**
5. Only if plan shows unexpected diff: investigate attribute change; never `state mv` for naming
6. `terraform destroy` is prohibited during this refactor

Resource names and Terraform addresses are frozen for this phase:

| Terraform address       | Physical name            |
|-------------------------|--------------------------|
| `aws_sqs_queue.tasks`   | `traceruntime-tasks`     |
| `aws_sqs_queue.dlq`     | `traceruntime-tasks-dlq` |
| `aws_s3_bucket.outputs` | `traceruntime-outputs`   |

Renaming physical resources has no operational benefit today and would force destroy/recreate, contradicting the goal of demonstrating safe IaC refactoring. If renaming becomes justified in the future, it belongs in a dedicated migration phase with rollback plan and consumer updates.

---

## Terraform File Structure

```
infra/terraform/
├── versions.tf               # required_version + required_providers (pinned)
├── providers.tf              # provider "aws" with LocalStack endpoints
├── sqs.tf                    # DLQ + main queue + redrive policy
├── s3.tf                     # artifact bucket
├── variables.tf              # localstack_endpoint, aws_region
├── outputs.tf                # task_queue_url, task_dlq_url, artifact_bucket_name, artifact_bucket_arn
├── terraform.tfvars.example  # override example (committed; terraform.tfvars is gitignored)
└── .terraform.lock.hcl       # committed — guarantees provider reproducibility
```

`main.tf` is removed after migration. No additional files (`locals.tf`, `data.tf`, `backend.tf`, `iam.tf`) — premature for current resource count.

The `backend "local"` block is omitted — Terraform already defaults to `terraform.tfstate` locally. Explicit configuration without gain is noise.

### versions.tf

```hcl
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

Provider pinned to `~> 5.0` — same major version currently in use. Upgrading to v6 during a structural refactor mixes concerns and risks breaking LocalStack compatibility. Provider major upgrade is a separate task.

### providers.tf

```hcl
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

### variables.tf

```hcl
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

### terraform.tfvars.example

```hcl
localstack_endpoint = "http://localhost:4566"
aws_region          = "us-east-1"
```

### sqs.tf

```hcl
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

`maxReceiveCount = 3` is intentionally low. Phase 8 and 9 exploit DLQ behavior for auto-healing and chaos experiments — masking failures with excessive retries defeats the purpose.

### s3.tf

```hcl
resource "aws_s3_bucket" "outputs" {
  bucket = "traceruntime-outputs"

  # LOCAL DEVELOPMENT ONLY
  # Never use force_destroy = true in production environments.
  force_destroy = true
}
```

### outputs.tf

```hcl
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

---

## SQS Operational Parameters

| Parameter                   | Value  | Notes                                        |
|-----------------------------|--------|----------------------------------------------|
| `visibility_timeout_seconds`| 150    | Provisional — recalibrate in Phase 8.5       |
| `message_retention_seconds` | 86400  | Main queue — 24h                             |
| `message_retention_seconds` | 604800 | DLQ — 7 days (operational inspection window) |
| `receive_wait_time_seconds` | 20     | Long polling                                 |
| `maxReceiveCount`           | 3      | 3 attempts -> DLQ (fast failure visibility)  |

---

## .gitignore

```gitignore
# Terraform state — never commit
*.tfstate
*.tfstate.*
.terraform/
terraform.tfvars
```

`.terraform.lock.hcl` is **committed** — it pins provider versions and guarantees that `terraform init` reproduces the exact same provider binary across machines and CI. Removing it from git defeats the purpose of `required_providers`.

---

## bootstrap.sh — Validate-Only Design

Terraform is the single source of truth for resource creation. Bootstrap no longer creates resources — it validates that Terraform has already provisioned them and fails loudly if anything is missing.

```
Terraform  ->  creates infrastructure
Bootstrap  ->  validates infrastructure exists
```

Behavior on every run:
```
==> Checking LocalStack is reachable... ok
==> Verifying traceruntime-tasks-dlq... ok
==> Verifying traceruntime-tasks... ok
==> Verifying traceruntime-outputs... ok
Bootstrap validation complete.
```

If a resource is missing, bootstrap exits with code 1:
```
==> Verifying traceruntime-tasks... MISSING
ERROR: Run 'make infra-apply' to provision infrastructure before starting the environment.
```

The script always uses `--endpoint-url` from `$LOCALSTACK_ENDPOINT` — never relies on ambient AWS credentials. This prevents accidental execution against a real AWS account.

---

## smoke-test.sh — Infrastructure Validation

Bash-only. No Go or Python dependency. Returns exit code 0 (all pass) or 1 (any failure). Continues all checks even after a failure — never exits early.

### Checks

```
 1. traceruntime-tasks exists
 2. traceruntime-tasks-dlq exists
 3. traceruntime-outputs exists
 4. visibility_timeout = 150
 5. receive_wait_time_seconds = 20
 6. maxReceiveCount = 3
 7. redrive policy points to correct DLQ ARN
 8. message sent to traceruntime-tasks
 9. message received from traceruntime-tasks
10. message deleted (cleanup)
11. S3 bucket accessible
```

### Output format

```
==> Smoke test: TraceRuntime infrastructure

v traceruntime-tasks exists
v traceruntime-tasks-dlq exists
v traceruntime-outputs exists
v visibility_timeout = 150
v receive_wait_time_seconds = 20
v maxReceiveCount = 3
v redrive policy configured correctly
v message sent to traceruntime-tasks (MessageId: abc123)
v message received from traceruntime-tasks
v message deleted (cleanup)
v S3 bucket accessible

Smoke test passed. (11/11)
```

On failure:
```
x traceruntime-tasks exists — queue not found
...
Smoke test FAILED. (10/11)
```

Exit codes: `0` = success, `1` = failure. No other exit codes.

---

## Makefile

Ports and endpoints defined as variables at the top — not hardcoded in targets.
`infra-init` is a shared prerequisite — avoids running `terraform init` redundantly in every target.

```makefile
# Config
LOCALSTACK_ENDPOINT   ?= http://localhost:4566
HEALTH_API_URL        ?= http://localhost:8082/health
HEALTH_WORKER_METRICS ?= http://localhost:9091/metrics
HEALTH_LOCALSTACK_URL ?= http://localhost:4566/_localstack/health

# Terraform
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

# Environment
.PHONY: up down restart reset

# First-time setup: run make infra-apply before make up
up:
	docker network create traceruntime 2>/dev/null || true
	docker compose -f infra/observability/docker-compose.yml up -d
	docker compose --profile no-ai up -d
	./scripts/bootstrap.sh

down:
	docker compose --profile no-ai down
	docker compose -f infra/observability/docker-compose.yml down

restart: down up

# reset tears down all state including infra — reprovisioning is required before up
reset:
	docker compose --profile no-ai down -v
	docker compose -f infra/observability/docker-compose.yml down -v
	docker network rm traceruntime 2>/dev/null || true
	$(MAKE) infra-apply
	$(MAKE) up

# Operations
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

# Development
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

### Startup flow

**First-time setup (git clone or full reset):**
```
make infra-apply   # provision SQS + S3 via Terraform (LocalStack must be running)
make up            # start all services; bootstrap validates resources
```

**Daily use:**
```
make up            # bootstrap validates; exits 1 with clear error if infra missing
```

**Full environment reset:**
```
make reset         # tears down all state, re-provisions infra, brings environment up
```

`make up` does not run `terraform apply` — startup stays fast and Terraform concerns stay separate.
`make reset` always re-provisions — it destroys everything including infra, so it must.

---

## Acceptance Criteria

- [ ] `terraform init` succeeds from clean checkout (no pre-existing `.terraform/`)
- [ ] `terraform fmt` — no diff
- [ ] `terraform validate` — no errors
- [ ] `terraform plan` — 0 add / 0 change / 0 destroy
- [ ] `make infra-apply` — succeeds against running LocalStack
- [ ] `make infra-bootstrap` — runs 3x with identical output; exits 1 if resource missing
- [ ] `make infra-smoke` — exit code 0, 11/11 checks pass
- [ ] `make up` — brings full environment up (after `make infra-apply`)
- [ ] `make down` — cleanly stops all services
- [ ] `make reset` — full teardown and reprovisioning in a single command
- [ ] `make queue-stats` — returns queue depth for main queue and DLQ
- [ ] No resource recreated during refactor (verified via `terraform plan`)
- [ ] `terraform.tfstate` not committed to git
- [ ] `.terraform/` not committed to git
- [ ] `.terraform.lock.hcl` committed to git

---

## What Is Not In This Phase

| Item                    | Reason deferred                                            |
|-------------------------|------------------------------------------------------------|
| SNS                     | No multi-consumer use case exists today                    |
| Resource renaming       | No operational benefit; would force destroy/recreate       |
| Terraform modules       | Only 3 resources — modules add structure without reuse     |
| Provider upgrade v5->v6 | Separate concern; risk of breaking LocalStack compatibility |
| CI Terraform validation | Belongs to CI/CD phase                                     |
| TFLint / Checkov        | Same                                                       |
| Multiple environments   | One operational environment today                          |
