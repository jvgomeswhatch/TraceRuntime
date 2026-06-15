---
name: otel:instrument-go
description: Adiciona SDK OpenTelemetry a um serviço Go existente — TraceProvider, spans em handlers HTTP e SQS, propagação W3C, exportação para OTEL Collector via gRPC. Preserva código existente, adiciona apenas instrumentação.
---

# Skill: otel:instrument-go

## Input necessário
1. Nome do serviço (ex: `order-service`)
2. O serviço tem handler HTTP, SQS consumer, ou ambos?
3. Endpoint do Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`, default `otel-collector:4317`)

## O que gerar

### 1. `internal/otelsetup/setup.go`
```go
package otelsetup

import (
    "context"
    "os"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
    endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if endpoint == "" {
        endpoint = "otel-collector:4317"
    }

    conn, err := grpc.DialContext(ctx, endpoint,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithBlock(),
    )
    if err != nil {
        return nil, fmt.Errorf("otel grpc dial: %w", err)
    }

    exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
    if err != nil {
        return nil, fmt.Errorf("otel exporter: %w", err)
    }

    res := resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName(serviceName),
        semconv.DeploymentEnvironment("local"),
    )

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exp),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sdktrace.AlwaysSample()),
    )

    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    return tp.Shutdown, nil
}
```

### 2. Span em handler HTTP (adicionar ao handler existente)
```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("order-service")

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.Start(r.Context(), "handler.create_order")
    defer span.End()

    // propagar context por todo o fluxo downstream
    // ...lógica existente...

    if err != nil {
        span.SetStatus(codes.Error, err.Error())
        span.RecordError(err)
        http.Error(w, "internal error", 500)
        return
    }

    span.SetAttributes(attribute.String("order.id", order.ID))
}
```

### 3. Span em SQS producer (ao enviar mensagem)
```go
func (p *Producer) Send(ctx context.Context, msg *Message) error {
    ctx, span := tracer.Start(ctx, "sqs.send")
    defer span.End()

    attrs := map[string]types.MessageAttributeValue{}
    injectTraceToSQS(ctx, attrs) // propagação W3C via MessageAttribute

    _, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
        QueueUrl:          &p.queueURL,
        MessageBody:       aws.String(string(body)),
        MessageAttributes: attrs,
    })
    if err != nil {
        span.SetStatus(codes.Error, err.Error())
        return err
    }
    return nil
}
```

### 4. Adicionar ao `main.go` (init + defer shutdown)
```go
shutdown, err := otelsetup.Init(ctx, "order-service")
if err != nil {
    logger.Error("otel init failed", "error", err)
    // continuar sem tracing — não fatal em dev
} else {
    defer shutdown(context.Background())
}
```

## Dependências go.mod a adicionar
```
go.opentelemetry.io/otel v1.24.0
go.opentelemetry.io/otel/sdk v1.24.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.24.0
go.opentelemetry.io/otel/semconv/v1.21.0 v1.24.0
google.golang.org/grpc v1.62.0
```

## Checklist pós-geração
- [ ] `OTEL_EXPORTER_OTLP_ENDPOINT` definido no docker-compose
- [ ] `otelsetup.Init` chamado no `main.go` antes de registrar handlers
- [ ] Spans adicionados nos pontos de I/O (HTTP in, SQS send, DB query)
- [ ] `r.Context()` / `ctx` propagado por toda a call chain — nunca `context.Background()` dentro de handler
- [ ] Trace aparece no Grafana Tempo após teste manual
- [ ] `go build ./...` sem erro
