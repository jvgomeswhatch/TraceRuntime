---
name: testing:integration-flow
description: Cria teste de integração end-to-end para um fluxo completo de domínio. Testa contra LocalStack e PostgreSQL reais. Sem mocks. Usa polling com timeout ao invés de Sleep fixo.
---

# Skill: testing:integration-flow

## Input necessário
1. Nome do fluxo (ex: `order-to-payment`, `payment-to-inventory`)
2. Evento inicial que dispara o fluxo
3. Estado final esperado (onde verificar o resultado?)
4. Timeout máximo aceitável para o fluxo completo

## O que gerar

### `tests/integration/{flow}_test.go`
```go
package integration_test

import (
    "context"
    "testing"
    "time"
    
    "github.com/stretchr/testify/require"
)

// TestOrderToPaymentFlow verifica que um pedido confirmado
// resulta em pagamento processado dentro do timeout
func TestOrderToPaymentFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Arrange — criar dados de teste
    orderID := createOrder(t, ctx, OrderInput{
        CustomerID: "test-customer-001",
        TotalCents: 10000,
    })
    
    // Act — disparar evento que inicia o fluxo
    publishEvent(t, ctx, "orders-confirmed", OrderConfirmedEvent{
        OrderID:    orderID,
        TotalCents: 10000,
        TraceID:    "test-trace-" + orderID,
    })
    
    // Assert — polling sem Sleep fixo
    payment := pollUntil[Payment](t, ctx, 10*time.Second, func() (*Payment, bool) {
        p := getPaymentByOrderID(t, ctx, orderID)
        if p == nil {
            return nil, false
        }
        return p, p.Status == "processed"
    })
    
    require.Equal(t, "processed", payment.Status)
    require.Equal(t, int64(10000), payment.TotalCents)
    
    // Cleanup
    t.Cleanup(func() {
        deleteOrder(t, context.Background(), orderID)
        deletePayment(t, context.Background(), payment.ID)
    })
}
```

### `tests/integration/helpers_test.go`
```go
package integration_test

import (
    "context"
    "testing"
    "time"
    "encoding/json"
    
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/jackc/pgx/v5/pgxpool"
)

var (
    db        *pgxpool.Pool
    sqsClient *sqs.Client
)

func TestMain(m *testing.M) {
    // Setup uma vez para todos os testes
    setup()
    code := m.Run()
    os.Exit(code)
}

// pollUntil — polling genérico com timeout (sem Sleep fixo)
func pollUntil[T any](t *testing.T, ctx context.Context, timeout time.Duration, fn func() (*T, bool)) *T {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if result, ok := fn(); ok {
            return result
        }
        select {
        case <-ctx.Done():
            t.Fatal("context cancelled while polling")
        case <-time.After(200 * time.Millisecond):
        }
    }
    t.Fatalf("condition not met within %s", timeout)
    return nil
}

// publishEvent — publica evento de teste no SQS (LocalStack)
func publishEvent(t *testing.T, ctx context.Context, queue string, payload any) {
    t.Helper()
    body, err := json.Marshal(Message{
        EventType: queue,
        TraceID:   "test-" + t.Name(),
        Timestamp: time.Now(),
        Payload:   mustMarshal(payload),
    })
    require.NoError(t, err)
    
    queueURL := getQueueURL(t, ctx, queue)
    _, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
        QueueUrl:    &queueURL,
        MessageBody: aws.String(string(body)),
    })
    require.NoError(t, err)
}
```

## Checklist pós-geração
- [ ] `TestMain` configura conexões reais (não mocks)
- [ ] `t.Cleanup` para todos os dados criados
- [ ] `testing.Short()` guard para CI rápido
- [ ] Timeout de contexto <= 30s por teste
- [ ] `pollUntil` com intervalo de 200ms (não mais curto — overhead)
- [ ] Teste de caminho de erro incluído (evento inválido → DLQ)
