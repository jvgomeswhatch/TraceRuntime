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
