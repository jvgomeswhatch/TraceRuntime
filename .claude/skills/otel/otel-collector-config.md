---
name: otel:collector-config
description: Gera o collector.yaml do OTEL Collector para este projeto — receivers OTLP gRPC/HTTP, processors de batch e memory_limiter, exporters para Tempo (traces), Prometheus (métricas) e Loki (logs). Calibrado para 14GB RAM total.
---

# Skill: otel:collector-config

## Input necessário
1. Quais serviços estão instrumentados? (para configurar scrape targets corretos)
2. Porta do Tempo gRPC (default `tempo:4317`)
3. Porta do Prometheus remote write (default `prometheus:9090`)

## O que gerar

### `infra/otel/collector.yaml`
```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317   # Go e Python services enviam aqui
      http:
        endpoint: 0.0.0.0:4318   # alternativa HTTP/protobuf

  # Scrape métricas dos serviços Go (Prometheus exposition format)
  prometheus:
    config:
      scrape_configs:
        - job_name: 'go-services'
          scrape_interval: 30s
          static_configs:
            - targets:
              - 'order-service:9090'
              - 'go-worker:9090'
        - job_name: 'ai-runtime'
          scrape_interval: 30s
          static_configs:
            - targets: ['ai-runtime:9091']
        - job_name: 'otel-collector-self'
          static_configs:
            - targets: ['localhost:8888']

processors:
  # Memory limiter: DEVE ser o primeiro processor
  memory_limiter:
    check_interval: 5s
    limit_mib: 100        # Collector max 128MB — manter margem
    spike_limit_mib: 20

  batch:
    timeout: 5s
    send_batch_size: 512     # conservador — sem bursts grandes
    send_batch_max_size: 1024

  # Remove atributos de alta cardinalidade antes de exportar métricas
  attributes/drop_ids:
    actions:
      - key: user_id
        action: delete
      - key: request_id
        action: delete

exporters:
  # Traces → Grafana Tempo
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true

  # Métricas → Prometheus (remote write)
  prometheusremotewrite:
    endpoint: http://prometheus:9090/api/v1/write
    tls:
      insecure_skip_verify: true

  # Logs → Loki
  loki:
    endpoint: http://loki:3100/loki/api/v1/push

  # Debug local (remover em staging)
  logging:
    verbosity: basic
    sampling_initial: 2
    sampling_thereafter: 500

extensions:
  health_check:
    endpoint: 0.0.0.0:13133
  pprof:
    endpoint: 0.0.0.0:1777    # profiling do Collector se necessário

service:
  extensions: [health_check, pprof]

  pipelines:
    traces:
      receivers:  [otlp]
      processors: [memory_limiter, batch]
      exporters:  [otlp/tempo, logging]

    metrics:
      receivers:  [otlp, prometheus]
      processors: [memory_limiter, attributes/drop_ids, batch]
      exporters:  [prometheusremotewrite]

    logs:
      receivers:  [otlp]
      processors: [memory_limiter, batch]
      exporters:  [loki]

  telemetry:
    metrics:
      address: 0.0.0.0:8888   # auto-observabilidade do Collector
```

### Serviço no `docker-compose.yml`
```yaml
otel-collector:
  image: otel/opentelemetry-collector-contrib:0.97.0
  command: ["--config=/etc/otel/collector.yaml"]
  volumes:
    - ./infra/otel/collector.yaml:/etc/otel/collector.yaml:ro
  ports:
    - "4317:4317"   # gRPC — serviços enviam traces aqui
    - "4318:4318"   # HTTP alternativo
    - "13133:13133" # health check
  mem_limit: 128m
  depends_on:
    - tempo
    - prometheus
    - loki
```

## Checklist pós-geração
- [ ] `infra/otel/` diretório criado
- [ ] collector.yaml adicionado ao volume no docker-compose
- [ ] `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` em todos os serviços
- [ ] `mem_limit: 128m` no serviço otel-collector
- [ ] Grafana data source Tempo apontando para `http://tempo:3200`
- [ ] `curl http://localhost:13133` retorna 200 após start
