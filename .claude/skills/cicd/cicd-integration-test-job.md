---
name: cicd-integration-test-job
description: Cria ou atualiza o job de testes de integração no pipeline CI, com LocalStack service container, bootstrap de infra e fluxo completo HTTP → SQS → worker → S3 → SSE. Use quando adicionar novos fluxos testáveis ou ajustar testes existentes.
---

# Skill: cicd-integration-test-job

## O que este job testa

Fluxo real end-to-end sem mocks:
```
HTTP POST /tasks → API → SQS → worker → S3 write → SSE event
```

## Estrutura do teste de integração

O test file vive em `tests/integration/flow_test.go`.

```go
package integration

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/stretchr/testify/require"
)

// Ambiente: API rodando, worker rodando, LocalStack saudável.
// Em CI: API e worker são binários iniciados no step de setup.
// Localmente: docker compose --profile no-ai up.

func TestTaskFlowEndToEnd(t *testing.T) {
    apiURL := getenv("API_URL", "http://localhost:8082")
    awsEndpoint := getenv("AWS_ENDPOINT_URL", "http://localhost:4566")
    s3Bucket := getenv("S3_BUCKET", "traceruntime-outputs")

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    // 1. Criar task via API
    body := `{"input": "integration test payload"}`
    resp, err := http.Post(apiURL+"/tasks", "application/json", strings.NewReader(body))
    require.NoError(t, err)
    require.Equal(t, http.StatusCreated, resp.StatusCode)

    var created struct {
        TaskID  string `json:"task_id"`
        TraceID string `json:"trace_id"`
    }
    require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
    resp.Body.Close()
    require.NotEmpty(t, created.TaskID)
    require.NotEmpty(t, created.TraceID)

    // 2. Aguardar output no S3 (worker processou)
    cfg, err := config.LoadDefaultConfig(ctx,
        config.WithEndpointResolverWithOptions(awsEndpointResolver(awsEndpoint)),
    )
    require.NoError(t, err)
    s3Client := s3.NewFromConfig(cfg)

    expectedKey := created.TraceID + "/" + created.TaskID + ".json"
    assertWithTimeout(t, ctx, 30*time.Second, func() bool {
        _, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
            Bucket: aws.String(s3Bucket),
            Key:    aws.String(expectedKey),
        })
        return err == nil
    })
}

func TestHealthEndpoints(t *testing.T) {
    apiURL := getenv("API_URL", "http://localhost:8082")
    workerURL := getenv("WORKER_URL", "http://localhost:9091")

    for _, url := range []string{apiURL + "/health", workerURL + "/health"} {
        resp, err := http.Get(url)
        require.NoError(t, err, "health check failed for %s", url)
        require.Equal(t, http.StatusOK, resp.StatusCode)
        resp.Body.Close()
    }
}

func assertWithTimeout(t *testing.T, ctx context.Context, timeout time.Duration, cond func() bool) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if cond() {
            return
        }
        select {
        case <-ctx.Done():
            t.Fatal("context cancelled waiting for condition")
        case <-time.After(500 * time.Millisecond):
        }
    }
    t.Fatal("condition not met within timeout")
}
```

## Como o CI inicia API e worker

No job de integração, API e worker são compilados e iniciados como processos background antes dos testes:

```yaml
- name: build binaries
  run: |
    cd services/api && go build -o /tmp/api-server ./cmd/server
    cd services/worker && go build -o /tmp/worker ./cmd/worker

- name: start api
  run: |
    QUEUE_BACKEND=sqs \
    SQS_QUEUE_URL=http://localhost:4566/000000000000/traceruntime-tasks \
    AWS_ENDPOINT_URL=http://localhost:4566 \
    OTEL_EXPORTER_OTLP_ENDPOINT="" \
    /tmp/api-server &
    echo $! > /tmp/api.pid
    sleep 3

- name: start worker
  run: |
    SQS_QUEUE_URL=http://localhost:4566/000000000000/traceruntime-tasks \
    SQS_DLQ_URL=http://localhost:4566/000000000000/traceruntime-tasks-dlq \
    S3_BUCKET=traceruntime-outputs \
    API_INTERNAL_URL=http://localhost:8082 \
    AI_RUNTIME_ENABLED=false \
    AWS_ENDPOINT_URL=http://localhost:4566 \
    OTEL_EXPORTER_OTLP_ENDPOINT="" \
    /tmp/worker &
    echo $! > /tmp/worker.pid
    sleep 3

- name: run integration tests
  working-directory: tests/integration
  run: go test -v -timeout 90s ./...
  env:
    API_URL: http://localhost:8082
    WORKER_URL: http://localhost:9091
    AWS_ENDPOINT_URL: http://localhost:4566
    S3_BUCKET: traceruntime-outputs
```

## Regras para este job

- OTEL desabilitado em CI (sem collector) — `OTEL_EXPORTER_OTLP_ENDPOINT=""`
- AI runtime desabilitado — `AI_RUNTIME_ENABLED=false` (mock path)
- Timeout total do teste: 90s (worker mock é instantâneo)
- Bootstrap roda antes de iniciar binários
