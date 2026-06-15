---
name: autohealing:requeue-task
description: Implementa requeue de tarefas SQS presas — usa ChangeMessageVisibility para tornar mensagens visíveis imediatamente sem apagá-las, com backoff exponencial e rastreamento de tentativas. Não perde mensagens, não cria duplicatas.
---

# Skill: autohealing:requeue-task

## Input necessário
1. Nome da fila SQS a monitorar
2. Visibility timeout atual das mensagens (para saber quando considerar "presa")
3. Máximo de tentativas de requeue por mensagem antes de mover para DLQ manualmente

## O que gerar

### `internal/requeue/requeuer.go`
```go
package requeue

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
    // Tornar mensagem visível imediatamente (0s = disponível agora)
    immediateVisibility = int32(0)
    // Máximo de tentativas antes de deixar expirar para DLQ
    maxRequeueAttempts = 3
)

type Requeuer struct {
    client   *sqs.Client
    queueURL string
    logger   *slog.Logger
}

func New(client *sqs.Client, queueURL string, logger *slog.Logger) *Requeuer {
    return &Requeuer{
        client:   client,
        queueURL: queueURL,
        logger:   logger,
    }
}

// RequeueStale torna visíveis mensagens em voo (in-flight) do worker stale.
// Usa ChangeMessageVisibility — não apaga, não duplica.
// Retorna o número de mensagens requeued.
func (r *Requeuer) RequeueStale(ctx context.Context, workerID string) (int, error) {
    // SQS não permite filtrar mensagens por worker — buscamos in-flight
    // (não visíveis) e tentamos torná-las visíveis novamente.
    // Nota: isso afeta TODAS mensagens in-flight, não apenas do workerID.
    // Para rastreamento fino, manter tabela de "mensagem → worker" no PostgreSQL.

    inflight, err := r.getInflightMessages(ctx)
    if err != nil {
        return 0, fmt.Errorf("get inflight: %w", err)
    }

    requeued := 0
    for _, msg := range inflight {
        approxReceive := getApproxReceiveCount(msg)
        if approxReceive >= maxRequeueAttempts {
            r.logger.Warn("requeue.skip_max_attempts",
                "message_id", *msg.MessageId,
                "receive_count", approxReceive,
                "worker_id", workerID)
            // Deixar expirar — SQS moverá para DLQ conforme maxReceiveCount
            continue
        }

        err := r.makeVisible(ctx, msg.ReceiptHandle)
        if err != nil {
            r.logger.Error("requeue.change_visibility_failed",
                "message_id", *msg.MessageId,
                "error", err,
                "worker_id", workerID)
            continue
        }

        r.logger.Info("requeue.ok",
            "message_id", *msg.MessageId,
            "receive_count", approxReceive,
            "worker_id", workerID)
        requeued++
    }

    return requeued, nil
}

// getInflightMessages busca mensagens atualmente in-flight (não visíveis).
// Requer permissão sqs:ReceiveMessage com VisibilityTimeout=0 para não mudar estado.
func (r *Requeuer) getInflightMessages(ctx context.Context) ([]types.Message, error) {
    // Estratégia: ReceiveMessage com VisibilityTimeout mínimo para "espiar"
    // sem adquirir lock permanente. Em LocalStack isso funciona igual à AWS.
    out, err := r.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
        QueueUrl:              &r.queueURL,
        MaxNumberOfMessages:   10, // máximo permitido pela API SQS
        WaitTimeSeconds:       0,  // sem long polling — snapshot rápido
        VisibilityTimeout:     5,  // 5s para decidir — devolver se não processar
        MessageAttributeNames: []string{"All"},
        AttributeNames:        []types.QueueAttributeName{"ApproximateReceiveCount"},
    })
    if err != nil {
        return nil, err
    }

    return out.Messages, nil
}

func (r *Requeuer) makeVisible(ctx context.Context, receiptHandle *string) error {
    _, err := r.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
        QueueUrl:          &r.queueURL,
        ReceiptHandle:     receiptHandle,
        VisibilityTimeout: immediateVisibility, // 0 = visível agora
    })
    return err
}

func getApproxReceiveCount(msg types.Message) int {
    val, ok := msg.Attributes["ApproximateReceiveCount"]
    if !ok {
        return 0
    }
    count := 0
    fmt.Sscanf(val, "%d", &count)
    return count
}
```

### Alternativa com rastreamento por worker (PostgreSQL)
```go
// Se precisar requeue apenas de mensagens do workerID específico:
// Manter tabela inflight_tasks no PostgreSQL

// CREATE TABLE inflight_tasks (
//     message_id     TEXT PRIMARY KEY,
//     receipt_handle TEXT NOT NULL,
//     worker_id      TEXT NOT NULL,
//     queue_url      TEXT NOT NULL,
//     acquired_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
// );

// Worker registra ao receber:
// INSERT INTO inflight_tasks (message_id, receipt_handle, worker_id, queue_url, acquired_at)
// VALUES ($1, $2, $3, $4, NOW())
// ON CONFLICT (message_id) DO UPDATE SET worker_id=$3, acquired_at=NOW()

// Watchdog busca por workerID específico:
// SELECT receipt_handle FROM inflight_tasks WHERE worker_id=$1

// Após ChangeMessageVisibility: DELETE FROM inflight_tasks WHERE message_id=$1
```

## Quando usar cada abordagem
| Situação | Abordagem |
|---|---|
| Worker stale genérico, 1 worker | ReceiveMessage com VisibilityTimeout=5 |
| Múltiplos workers, rastreamento fino | Tabela `inflight_tasks` PostgreSQL |
| Mensagem específica travada | ChangeMessageVisibility direto com receipt_handle |

## Checklist pós-geração
- [ ] `Requeuer` implementa interface `watchdog.Requeuer`
- [ ] `immediateVisibility = 0` — não colocar valor positivo (readia delay)
- [ ] `maxRequeueAttempts < SQS maxReceiveCount` — DLQ ainda funciona após max tentativas
- [ ] Log inclui `message_id` e `worker_id` para correlação
- [ ] Métrica `requeue_attempts_total{status}` incrementada
- [ ] Testado em LocalStack: `aws sqs change-message-visibility` deve funcionar igual
