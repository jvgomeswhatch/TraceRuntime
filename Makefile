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
.PHONY: bootstrap up down restart reset

# First-time setup or reprovisioning — idempotent, safe to run multiple times
bootstrap:
	docker network create traceruntime 2>/dev/null || true
	docker compose up -d localstack postgres
	docker compose --profile infra up terraform
	docker compose --profile no-ai up -d
	docker exec traceruntime-localstack-1 awslocal sqs get-queue-url --queue-name traceruntime-tasks --output text
	docker exec traceruntime-localstack-1 awslocal sqs get-queue-url --queue-name traceruntime-tasks-dlq --output text
	docker exec traceruntime-localstack-1 awslocal s3api head-bucket --bucket traceruntime-outputs
	@echo "Bootstrap validation complete."

# Daily development — assumes bootstrap has been run at least once
up:
	docker network create traceruntime 2>/dev/null || true
	docker compose -f infra/observability/docker-compose.yml up -d
	docker compose --profile no-ai up -d

up-full:
	docker network create traceruntime 2>/dev/null || true
	docker compose -f infra/observability/docker-compose.yml up -d
	docker compose --profile full up -d

down:
	docker compose --profile full down
	docker compose -f infra/observability/docker-compose.yml down

restart: down up

# reset tears down all state including volumes — requires bootstrap after
reset:
	docker compose --profile no-ai down -v
	docker compose --profile infra down -v
	docker compose -f infra/observability/docker-compose.yml down -v
	docker network rm traceruntime 2>/dev/null || true
	$(MAKE) bootstrap

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
	@curl -sf http://localhost:9093/health > /dev/null && echo " watchdog: ok" || echo " watchdog: FAIL"

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
.PHONY: test test-go test-python lint lint-go lint-terraform lint-python lint-frontend fmt

test: test-go test-python

test-go:
	cd services/api && go test ./...
	cd services/worker && go test ./...
	cd services/watchdog && go test ./...

test-python:
	cd ai-runtime && python -m pytest -v

lint: lint-go lint-terraform lint-python lint-frontend

lint-go:
	cd services/api && golangci-lint run ./...
	cd services/worker && golangci-lint run ./...
	cd services/watchdog && golangci-lint run ./...

lint-terraform:
	cd infra/terraform && terraform fmt -check -recursive
	cd infra/terraform && terraform validate

lint-python:
	cd ai-runtime && ruff check .

lint-frontend:
	cd frontend && npm run lint
	cd frontend && npm run typecheck

fmt:
	gofmt -w .
	cd infra/terraform && terraform fmt -recursive

# ── Load Testing ──────────────────────────────────────────────────────────────
.PHONY: loadtest

loadtest:
	cd cmd/loadtest && go run . \
		--tasks=$(or $(TASKS),50) \
		--rate=$(or $(RATE),2) \
		--concurrency=$(or $(CONCURRENCY),4) \
		--output-dir=../../results
