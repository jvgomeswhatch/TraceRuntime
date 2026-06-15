---
name: observability:go-metrics
description: Adiciona instrumentação Prometheus completa a um serviço Go existente — histograma de latência, contadores SQS, gauge de goroutines ativas e endpoint /metrics. Métricas low-cardinality apenas.
---

# Skill: observability:go-metrics

## Input necessário
1. Nome do serviço
2. É HTTP server, SQS consumer, ou ambos?
3. Métricas de negócio específicas? (ex: `orders_created_total`)

## O que gerar

### `internal/metrics/metrics.go`
```go
package metrics

import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    // HTTP — apenas para serviços com API
    HTTPDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Help:    "HTTP request duration",
            Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
        },
        []string{"method", "path", "status"}, // NUNCA adicionar user_id aqui
    )

    // SQS Consumer
    SQSMessagesProcessed = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "sqs_messages_processed_total",
            Help: "Total SQS messages processed",
        },
        []string{"queue", "status"}, // status: success | error | invalid
    )

    SQSProcessingDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "sqs_message_processing_duration_seconds",
            Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5},
        },
        []string{"queue"},
    )

    // Recurso
    ActiveWorkers = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "worker_pool_active",
        Help: "Active workers in pool",
    })
)

func Register() {
    prometheus.MustRegister(
        HTTPDuration,
        SQSMessagesProcessed,
        SQSProcessingDuration,
        ActiveWorkers,
    )
}

func Handler() http.Handler {
    return promhttp.Handler()
}
```

### Uso no consumer (medir processamento)
```go
import "service/internal/metrics"

func (c *Consumer) process(ctx context.Context, msg *Message) {
    metrics.ActiveWorkers.Inc()
    defer metrics.ActiveWorkers.Dec()

    timer := prometheus.NewTimer(
        metrics.SQSProcessingDuration.WithLabelValues(c.queueName),
    )
    defer timer.ObserveDuration()

    if err := c.handler.Handle(ctx, msg); err != nil {
        metrics.SQSMessagesProcessed.WithLabelValues(c.queueName, "error").Inc()
        return
    }
    metrics.SQSMessagesProcessed.WithLabelValues(c.queueName, "success").Inc()
}
```

### Registrar endpoint /metrics no main.go
```go
mux := http.NewServeMux()
mux.Handle("/metrics", metrics.Handler())
mux.HandleFunc("/health", healthHandler)

// Servidor de métricas em porta separada — não exposta publicamente
go func() {
    if err := http.ListenAndServe(":9090", mux); err != nil {
        logger.Error("metrics server error", "error", err)
    }
}()
```

## Regras de cardinalidade (OBRIGATÓRIO)
- Labels permitidos: `queue`, `method`, `path`, `status` (string fixa)
- Labels PROIBIDOS: `user_id`, `order_id`, `trace_id`, qualquer UUID
- Máximo 10 combinações de labels por métrica

## Checklist pós-geração
- [ ] `metrics.Register()` chamado no `main.go`
- [ ] Porta 9090 exposta no docker-compose para Prometheus scrape
- [ ] Prometheus config atualizado com novo target
- [ ] Sem labels com alta cardinalidade
