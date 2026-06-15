---
name: cicd-pipeline-scaffold
description: Cria o .github/workflows/ci.yml completo para o TraceRuntime — lint, test unitário, integração com LocalStack, build de imagens Docker, validação Terraform. Use quando iniciar CI no projeto ou quando o pipeline precisar ser recriado do zero.
---

# Skill: cicd-pipeline-scaffold

## Quando usar

Quando o agente cicd for criar o pipeline inicial do projeto.

## Pré-condições obrigatórias

Antes de criar o workflow, verificar:

1. `services/api/` existe e tem `go.mod`
2. `services/worker/` existe e tem `go.mod` (Phase 6 Task 3+)
3. `ai-runtime/` existe e tem `requirements.txt`
4. `infra/terraform/` existe com `main.tf` (Phase 7+)
5. `scripts/bootstrap.sh` existe

Se algum desses não existe, o job correspondente deve ser omitido ou marcado como condicional.

## Estrutura do pipeline

```
on: [push, pull_request] → branch main

jobs:
  lint-go          → paralelo
  lint-python      → paralelo
  test-unit        → após lint-go
  test-integration → após test-unit, precisa LocalStack
  build-images     → após test-integration
  validate-infra   → após test-integration, precisa LocalStack
```

## Conteúdo do workflow

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  AWS_ACCESS_KEY_ID: test
  AWS_SECRET_ACCESS_KEY: test
  AWS_DEFAULT_REGION: us-east-1
  LOCALSTACK_ENDPOINT: http://localhost:4566

jobs:
  lint-go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache-dependency-path: |
            services/api/go.sum
            services/worker/go.sum
      - name: vet api
        working-directory: services/api
        run: go vet ./...
      - name: vet worker
        working-directory: services/worker
        run: go vet ./...

  lint-python:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: "3.11"
      - run: pip install ruff
      - name: ruff ai-runtime
        working-directory: ai-runtime
        run: ruff check .

  test-unit:
    runs-on: ubuntu-latest
    needs: lint-go
    env:
      QUEUE_BACKEND: inmemory
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache-dependency-path: |
            services/api/go.sum
            services/worker/go.sum
      - name: test api
        working-directory: services/api
        run: go test -race -count=1 ./...
      - name: test worker
        working-directory: services/worker
        run: go test -race -count=1 ./...

  test-integration:
    runs-on: ubuntu-latest
    needs: test-unit
    services:
      localstack:
        image: localstack/localstack:3.4
        ports:
          - "4566:4566"
        env:
          SERVICES: sqs,s3
        options: >-
          --health-cmd "curl -sf http://localhost:4566/_localstack/health"
          --health-interval 5s
          --health-timeout 3s
          --health-retries 10
    env:
      QUEUE_BACKEND: sqs
      SQS_QUEUE_URL: http://localhost:4566/000000000000/traceruntime-tasks
      SQS_DLQ_URL: http://localhost:4566/000000000000/traceruntime-tasks-dlq
      S3_BUCKET: traceruntime-outputs
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache-dependency-path: |
            services/api/go.sum
            services/worker/go.sum
      - name: install awscli
        run: pip install awscli
      - name: bootstrap localstack
        run: bash scripts/bootstrap.sh
      - name: integration tests
        working-directory: tests/integration
        run: go test -v -timeout 120s ./...
        env:
          API_URL: http://localhost:8082
          AWS_ENDPOINT_URL: http://localhost:4566

  build-images:
    runs-on: ubuntu-latest
    needs: test-integration
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - name: build api
        uses: docker/build-push-action@v5
        with:
          context: services/api
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max
      - name: build worker
        uses: docker/build-push-action@v5
        with:
          context: services/worker
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max
      - name: build ai-runtime
        uses: docker/build-push-action@v5
        with:
          context: ai-runtime
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max

  validate-infra:
    runs-on: ubuntu-latest
    needs: test-unit
    services:
      localstack:
        image: localstack/localstack:3.4
        ports:
          - "4566:4566"
        env:
          SERVICES: sqs,s3
        options: >-
          --health-cmd "curl -sf http://localhost:4566/_localstack/health"
          --health-interval 5s
          --health-timeout 3s
          --health-retries 10
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: "1.9.0"
      - name: terraform init
        working-directory: infra/terraform
        run: terraform init
      - name: terraform validate
        working-directory: infra/terraform
        run: terraform validate
      - name: terraform plan
        working-directory: infra/terraform
        run: terraform plan -detailed-exitcode
        env:
          TF_VAR_localstack_endpoint: http://localhost:4566
```

## Checklist pós-criação

- [ ] Verificar se `.github/workflows/` existe, criar se necessário
- [ ] Confirmar que `tests/integration/` tem `go.mod` próprio
- [ ] Confirmar que `scripts/bootstrap.sh` é executável (`chmod +x`)
- [ ] Verificar se `infra/terraform/` existe antes de incluir `validate-infra`
- [ ] Omitir jobs de serviços que ainda não existem no repo
