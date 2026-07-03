SHELL := /bin/bash

# ── Config ────────────────────────────────────────────────────────────────────
LOCALSTACK_ENDPOINT   ?= http://localhost:4566
HEALTH_API_URL        ?= http://localhost:8082/health
HEALTH_WORKER_METRICS ?= http://localhost:9091/metrics
HEALTH_LOCALSTACK_URL ?= http://localhost:4566/_localstack/health
TERRAFORM             ?= $(shell which terraform 2>/dev/null || echo /c/terraform/terraform.exe)

# ── Terraform ─────────────────────────────────────────────────────────────────
.PHONY: infra-init infra-plan infra-apply infra-destroy infra-bootstrap infra-fmt infra-validate infra-smoke

infra-init:
	cd infra/terraform && $(TERRAFORM) init -input=false

infra-plan: infra-init
	cd infra/terraform && $(TERRAFORM) plan

infra-apply: infra-init
	cd infra/terraform && $(TERRAFORM) apply -auto-approve

infra-destroy: infra-init
	cd infra/terraform && $(TERRAFORM) destroy -auto-approve

infra-fmt:
	cd infra/terraform && $(TERRAFORM) fmt -recursive

infra-validate: infra-init
	cd infra/terraform && $(TERRAFORM) validate

infra-bootstrap:
	./scripts/bootstrap.sh

infra-smoke:
	/bin/bash scripts/smoke-test.sh

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

build:
	docker compose --profile full build

rebuild:
	docker compose --profile full up -d --build

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

# clean wipes all data (DB, SQS, S3, results) without touching containers or infra
clean:
	@echo "Cleaning PostgreSQL..."
	@docker exec traceruntime-postgres-1 psql -U traceruntime -d traceruntime -c \
		"TRUNCATE tasks, healing_events, worker_heartbeats, chaos_runs CASCADE" 2>/dev/null \
		&& echo "  tables truncated" || echo "  SKIP (postgres not running)"
	@echo "Purging SQS queues..."
	@docker exec traceruntime-localstack-1 awslocal sqs purge-queue \
		--queue-url http://localstack:4566/000000000000/traceruntime-tasks 2>/dev/null \
		&& echo "  tasks queue purged" || echo "  SKIP (localstack not running)"
	@docker exec traceruntime-localstack-1 awslocal sqs purge-queue \
		--queue-url http://localstack:4566/000000000000/traceruntime-tasks-dlq 2>/dev/null \
		&& echo "  DLQ purged" || echo "  SKIP"
	@echo "Clearing S3 bucket..."
	@docker exec traceruntime-localstack-1 awslocal s3 rm s3://traceruntime-outputs --recursive 2>/dev/null \
		&& echo "  bucket cleared" || echo "  SKIP (localstack not running)"
	@echo "Removing local results..."
	@rm -f results/loadtest-*.json results/chaos-suite-*.json
	@rm -f results/chaos-requests/*.json
	@echo "Clean complete."

# ── Operations ─────────────────────────────────────────────────────────────────
.PHONY: ps logs health queue-stats clean

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
	cd infra/terraform && $(TERRAFORM) fmt -check -recursive
	cd infra/terraform && $(TERRAFORM) validate

lint-python:
	cd ai-runtime && ruff check .

lint-frontend:
	cd frontend && npm run lint
	cd frontend && npm run typecheck

fmt:
	gofmt -w .
	cd infra/terraform && $(TERRAFORM) fmt -recursive

# ── Load Testing ──────────────────────────────────────────────────────────────
.PHONY: loadtest

loadtest:
	cd cmd/loadtest && go run . \
		--tasks=$(or $(TASKS),50) \
		--rate=$(or $(RATE),2) \
		--concurrency=$(or $(CONCURRENCY),4) \
		--output-dir=../../results

# ── Integration Testing ──────────────────────────────────────────────────────
.PHONY: test-integration test-integration-up test-integration-down test-all

test-integration-up:
	docker compose --profile no-ai -f docker-compose.yml -f docker-compose.test.yml up -d --build
	@echo "Waiting for services..."
	@until curl -sf http://localhost:8082/health > /dev/null 2>&1; do sleep 2; done
	@echo "Services ready."

test-integration-down:
	docker compose --profile no-ai -f docker-compose.yml -f docker-compose.test.yml down

test-integration:
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make test-integration-up' first." && exit 1)
	cd tests/integration && go test -v -count=1 -timeout=5m ./...

test-all: test test-integration

# ── Chaos Testing ─────────────────────────────────────────────────────────────
.PHONY: chaos-build chaos-up chaos-down chaos-reset chaos

chaos-build:
	docker compose --profile full build

chaos-up:
	@openssl rand -hex 32 > .chaos.token
	docker network create traceruntime 2>/dev/null || true
	docker compose -f infra/observability/docker-compose.yml up -d
	CHAOS_ENABLED=true CHAOS_INTERNAL_TOKEN=$$(cat .chaos.token) \
		docker compose --profile full up -d
	@aws --endpoint-url=http://localhost:4566 sqs purge-queue \
		--queue-url http://localhost:4566/000000000000/traceruntime-tasks-dlq 2>/dev/null || true
	@echo "Chaos token persisted to .chaos.token"

chaos-down:
	@aws --endpoint-url=http://localhost:4566 sqs purge-queue \
		--queue-url http://localhost:4566/000000000000/traceruntime-tasks-dlq 2>/dev/null || true
	docker compose --profile full down
	docker compose -f infra/observability/docker-compose.yml down
	@rm -f .chaos.token

chaos-reset:
	docker compose --profile full down -v
	docker compose -f infra/observability/docker-compose.yml down -v
	@rm -f .chaos.token
	$(MAKE) bootstrap

chaos:
ifdef LIST
	cd cmd/chaos && go run . --list
else
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make chaos-up' first." && exit 1)
	@test -f .chaos.token || (echo "ERROR: .chaos.token not found. Run 'make chaos-up' first." && exit 1)
	$(eval CHAOS_SCENARIO := $(or $(SCENARIO),all))
	$(eval CHAOS_RUN_ID := $(or $(RUN_ID),chaos-$(CHAOS_SCENARIO)-$(shell date +%Y%m%d-%H%M%S)))
	cd cmd/chaos && CHAOS_INTERNAL_TOKEN=$$(cat ../../.chaos.token) go run . \
		--output-dir=../../$(or $(OUTPUT_DIR),results) \
		$(if $(SCENARIO),--scenario=$(SCENARIO),) \
		--run-id=$(CHAOS_RUN_ID)
endif
