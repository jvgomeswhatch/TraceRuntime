---
name: go-services:new-consumer
description: Scaffold completo de um consumer SQS em Go com worker pool bounded, graceful shutdown, DLQ handling e métricas Prometheus. Use sempre que criar um novo consumer de fila.
---

# Skill: go-services:new-consumer

## Input necessário
Antes de começar, confirme:
1. Nome do serviço (ex: `payment-service`)
2. Nome da fila SQS (ex: `orders-confirmed`)
3. Nome do event type esperado (ex: `order.confirmed`)
4. Payload struct esperado

## O que gerar

### 1. `cmd/consumer/main.go`
```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer stop()

    sqsEndpoint := os.Getenv("SQS_ENDPOINT")
    queueURL := os.Getenv("QUEUE_URL")

    cfg, err := config.LoadDefaultConfig(ctx,
        config.WithEndpointResolverWithOptions(endpointResolver(sqsEndpoint)),
    )
    if err != nil {
        logger.Error("failed to load aws config", "error", err)
        os.Exit(1)
    }

    client := sqs.NewFromConfig(cfg)
    consumer := NewConsumer(client, queueURL, logger)

    if err := consumer.Run(ctx); err != nil {
        logger.Error("consumer error", "error", err)
        os.Exit(1)
    }
}
```

### 2. `internal/consumer/consumer.go`
```go
package consumer

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
    maxWorkers     = 3   // nunca mais que isso — memória restrita
    maxMessages    = 5   // batch size conservador
    visibilityTimeout = 30
)

type Consumer struct {
    client   *sqs.Client
    queueURL string
    logger   *slog.Logger
    handler  MessageHandler
}

type MessageHandler interface {
    Handle(ctx context.Context, msg *Message) error
}

func (c *Consumer) Run(ctx context.Context) error {
    sem := make(chan struct{}, maxWorkers)
    var wg sync.WaitGroup

    for {
        select {
        case <-ctx.Done():
            wg.Wait()
            return nil
        default:
        }

        out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
            QueueUrl:            &c.queueURL,
            MaxNumberOfMessages: maxMessages,
            WaitTimeSeconds:     20, // long polling — não busy wait
            VisibilityTimeout:   visibilityTimeout,
        })
        if err != nil {
            c.logger.Error("receive error", "error", err)
            time.Sleep(2 * time.Second)
            continue
        }

        for _, sqsMsg := range out.Messages {
            sem <- struct{}{}
            wg.Add(1)
            go func(m types.Message) {
                defer wg.Done()
                defer func() { <-sem }()
                c.process(ctx, m)
            }(sqsMsg)
        }
    }
}

func (c *Consumer) process(ctx context.Context, sqsMsg types.Message) {
    var msg Message
    if err := json.Unmarshal([]byte(*sqsMsg.Body), &msg); err != nil {
        c.logger.Error("invalid message format", "error", err)
        return // deixa expirar visibility — vai pra DLQ
    }

    if err := validateMessage(&msg); err != nil {
        c.logger.Error("message validation failed", "error", err, "trace_id", msg.TraceID)
        c.deleteMessage(ctx, sqsMsg.ReceiptHandle)
        return
    }

    if err := c.handler.Handle(ctx, &msg); err != nil {
        c.logger.Error("handler error", "error", err, "trace_id", msg.TraceID)
        return // não deleta — vai pra DLQ após maxReceiveCount
    }

    c.deleteMessage(ctx, sqsMsg.ReceiptHandle)
}

func (c *Consumer) deleteMessage(ctx context.Context, receiptHandle *string) {
    _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
        QueueUrl:      &c.queueURL,
        ReceiptHandle: receiptHandle,
    })
    if err != nil {
        c.logger.Error("delete message error", "error", err)
    }
}
```

## Checklist pós-geração
- [ ] `QUEUE_URL` e `SQS_ENDPOINT` definidos no docker-compose
- [ ] DLQ configurada no módulo Terraform
- [ ] Handler implementado em `internal/handler/`
- [ ] Teste de integração criado em `tests/integration/`
- [ ] `mem_limit: 128M` no serviço no docker-compose
