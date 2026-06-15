---
name: go-services:worker
description: Worker Go completo com heartbeat loop, graceful shutdown via context, worker pool bounded, integração com fila local (Phase 2) e SQS (Phase 6+), e integração com heartbeat tracker para auto-healing.
---

# Skill: go-services:worker

## Input necessário
1. Nome do worker (ex: `event-processor`)
2. Fonte da fila: local channel (Phase 2) ou SQS (Phase 6+)?
3. O worker chama o AI runtime? Sim/Não
4. Máximo de workers paralelos (default: 3, respeitar 128MB limit)

## O que gerar

### `cmd/worker/main.go`
```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"

    "yourmodule/internal/consumer"
    "yourmodule/internal/handler"
    "yourmodule/internal/heartbeat"
    "yourmodule/internal/metrics"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))

    ctx, stop := signal.NotifyContext(context.Background(),
        syscall.SIGTERM, syscall.SIGINT)
    defer stop()

    // Database
    db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil {
        logger.Error("db.connect_failed", "error", err)
        os.Exit(1)
    }
    defer db.Close()

    // Metrics
    metrics.Register()
    go metrics.ServeHTTP(":9090") // porta separada — não exposta publicamente

    // Heartbeat tracker (auto-healing Phase 8)
    tracker := heartbeat.New(db, logger)
    go tracker.Run(ctx)

    logger.Info("worker.starting",
        "worker_id", tracker.WorkerID(),
        "queue", os.Getenv("QUEUE_URL"))

    // Consumer (SQS ou local queue — depende da fase)
    c := consumer.New(
        os.Getenv("SQS_ENDPOINT"),
        os.Getenv("QUEUE_URL"),
        handler.New(db, logger),
        logger,
    )

    // Bloqueia até ctx cancelado (SIGTERM/SIGINT)
    if err := c.Run(ctx); err != nil {
        logger.Error("worker.run_error", "error", err)
        os.Exit(1)
    }

    logger.Info("worker.stopped_gracefully")
}
```

### `internal/consumer/consumer.go` (com bounded pool)
```go
package consumer

import (
    "context"
    "encoding/json"
    "log/slog"
    "sync"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/service/sqs/types"

    "yourmodule/internal/handler"
    "yourmodule/internal/metrics"
    "yourmodule/internal/tracing"
)

const (
    maxWorkers      = 3   // hard limit — memória restrita (128MB)
    maxMessages     = 5   // batch SQS conservador
    visibilityTimeout = 30
    pollWait        = 20  // long polling — não busy-wait
    errorBackoff    = 2 * time.Second
)

type Consumer struct {
    client   *sqs.Client
    queueURL string
    handler  handler.Handler
    logger   *slog.Logger
}

func New(sqsEndpoint, queueURL string, h handler.Handler, logger *slog.Logger) *Consumer {
    cfg, _ := config.LoadDefaultConfig(context.Background(),
        config.WithEndpointResolverWithOptions(
            aws.EndpointResolverWithOptionsFunc(
                func(service, region string, options ...interface{}) (aws.Endpoint, error) {
                    return aws.Endpoint{URL: sqsEndpoint}, nil
                },
            ),
        ),
    )
    return &Consumer{
        client:   sqs.NewFromConfig(cfg),
        queueURL: queueURL,
        handler:  h,
        logger:   logger,
    }
}

func (c *Consumer) Run(ctx context.Context) error {
    sem := make(chan struct{}, maxWorkers)
    var wg sync.WaitGroup

    for {
        select {
        case <-ctx.Done():
            wg.Wait() // aguardar mensagens em processamento
            return nil
        default:
        }

        out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
            QueueUrl:              &c.queueURL,
            MaxNumberOfMessages:   maxMessages,
            WaitTimeSeconds:       pollWait,
            VisibilityTimeout:     visibilityTimeout,
            MessageAttributeNames: []string{"All"}, // necessário para traceparent
            AttributeNames:        []types.QueueAttributeName{"ApproximateReceiveCount"},
        })
        if err != nil {
            if ctx.Err() != nil {
                return nil // shutdown normal
            }
            c.logger.Error("sqs.receive_error", "error", err)
            time.Sleep(errorBackoff)
            continue
        }

        for _, sqsMsg := range out.Messages {
            sem <- struct{}{}
            wg.Add(1)
            metrics.ActiveWorkers.Inc()

            go func(m types.Message) {
                defer wg.Done()
                defer func() { <-sem }()
                defer metrics.ActiveWorkers.Dec()
                c.process(ctx, m)
            }(sqsMsg)
        }
    }
}

func (c *Consumer) process(ctx context.Context, sqsMsg types.Message) {
    // Extrair trace context ANTES de criar span
    ctx = tracing.ExtractFromSQSAttrs(ctx, sqsMsg.MessageAttributes)

    timer := metrics.SQSProcessingDuration.MustCurryWith(nil)
    _ = timer // usar quando tiver fila nomeada

    var msg Message
    if err := json.Unmarshal([]byte(*sqsMsg.Body), &msg); err != nil {
        c.logger.Error("msg.unmarshal_error", "error", err,
            "message_id", *sqsMsg.MessageId)
        // Não deleta — vai para DLQ após maxReceiveCount
        metrics.SQSMessagesProcessed.WithLabelValues(c.queueURL, "invalid").Inc()
        return
    }

    if err := c.handler.Handle(ctx, &msg); err != nil {
        c.logger.Error("msg.handler_error", "error", err,
            "trace_id", msg.TraceID, "event_type", msg.EventType)
        metrics.SQSMessagesProcessed.WithLabelValues(c.queueURL, "error").Inc()
        return // não deleta — retry via SQS
    }

    c.deleteMessage(ctx, sqsMsg.ReceiptHandle)
    metrics.SQSMessagesProcessed.WithLabelValues(c.queueURL, "success").Inc()
}

func (c *Consumer) deleteMessage(ctx context.Context, receiptHandle *string) {
    _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
        QueueUrl:      &c.queueURL,
        ReceiptHandle: receiptHandle,
    })
    if err != nil {
        c.logger.Error("sqs.delete_error", "error", err)
    }
}

type Message struct {
    EventType string          `json:"event_type"`
    TraceID   string          `json:"trace_id"`
    Timestamp time.Time       `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`
}
```

### docker-compose snippet para o worker
```yaml
go-worker:
  build:
    context: ./services/worker
    dockerfile: Dockerfile
  mem_limit: 128m
  memswap_limit: 128m
  environment:
    SQS_ENDPOINT: "http://localstack:4566"
    QUEUE_URL: "http://localstack:4566/000000000000/events-queue"
    DATABASE_URL: "postgres://user:pass@postgres:5432/eventdrive"
    WORKER_ID: "go-worker-1"
    OTEL_EXPORTER_OTLP_ENDPOINT: "otel-collector:4317"
  depends_on:
    localstack:
      condition: service_healthy
    postgres:
      condition: service_healthy
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:9090/health"]
    interval: 10s
    timeout: 3s
    retries: 3
  restart: unless-stopped
```

## Checklist pós-geração
- [ ] `maxWorkers = 3` — ajustar se serviço tiver mais memória disponível
- [ ] `signal.NotifyContext` para SIGTERM/SIGINT — graceful shutdown Docker
- [ ] `wg.Wait()` após ctx cancelado — aguardar mensagens em flight
- [ ] `MessageAttributeNames: []string{"All"}` — obrigatório para traceparent SQS
- [ ] `heartbeat.Run` em goroutine — watchdog pode detectar stale
- [ ] `mem_limit: 128m` no docker-compose
- [ ] `restart: unless-stopped` no docker-compose
- [ ] `go build ./...` antes de declarar pronto
