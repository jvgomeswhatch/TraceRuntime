---
name: observability:alert-slo
description: Alertas Prometheus para SLOs críticos deste projeto — p95 latency, queue lag, worker heartbeat ausente, container OOM. Inclui regras PromQL, thresholds calibrados para o ambiente local e integração com Grafana alerting.
---

# Skill: observability:alert-slo

## Input necessário
1. Quais SLOs habilitar? (latency, queue, heartbeat, oom — default: todos)
2. Para onde enviar alertas? Grafana UI apenas (dev) ou webhook externo?

## O que gerar

### `infra/prometheus/rules/slo.yml`
```yaml
groups:
  - name: eventdrive.slo
    interval: 30s  # avaliar a cada 30s — mesma frequência do scrape

    rules:
      # ── HTTP Latency ──────────────────────────────────────────────
      - alert: HTTPLatencyP95High
        expr: |
          histogram_quantile(0.95,
            sum(rate(http_request_duration_seconds_bucket[5m])) by (le, service)
          ) > 2.0
        for: 2m  # alertar apenas se persistir 2min — evitar flapping
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "p95 latency above 2s on {{ $labels.service }}"
          description: "p95={{ $value | humanizeDuration }} for service={{ $labels.service }}. Check Tempo for slow traces."

      # ── Queue Lag ─────────────────────────────────────────────────
      - alert: QueueLagHigh
        expr: |
          sqs_approximate_number_of_messages_visible > 100
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Queue lag above 100 messages"
          description: "{{ $value }} messages in queue. Worker may be stale or processing slowly."

      - alert: DLQMessageArrived
        expr: |
          increase(sqs_approximate_number_of_messages_visible{queue=~".*dlq.*"}[5m]) > 0
        for: 0m  # alertar imediatamente — DLQ não deve ter mensagens
        labels:
          severity: critical
        annotations:
          summary: "Messages arrived in DLQ"
          description: "{{ $value }} messages in DLQ in last 5min. Investigate handler errors."

      # ── Worker Heartbeat ──────────────────────────────────────────
      - alert: WorkerHeartbeatMissing
        expr: |
          (time() - worker_last_heartbeat_timestamp_seconds) > 30
        for: 0m  # imediato — heartbeat é crítico para auto-healing
        labels:
          severity: critical
        annotations:
          summary: "Worker heartbeat missing for {{ $labels.worker_id }}"
          description: "No heartbeat for {{ $value | humanizeDuration }}. Watchdog should be detecting and requeueing."

      # ── Container Memory ──────────────────────────────────────────
      - alert: ContainerMemoryHigh
        expr: |
          (container_memory_usage_bytes{name=~"go-worker|order-service|ai-runtime|otel-collector"}
           / container_spec_memory_limit_bytes{name=~"go-worker|order-service|ai-runtime|otel-collector"})
          > 0.85
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Container {{ $labels.name }} memory above 85%"
          description: "{{ $value | humanizePercentage }} of mem_limit used. Risk of OOM kill."

      - alert: ContainerOOMKilled
        expr: |
          increase(container_oom_events_total[5m]) > 0
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "Container OOM killed"
          description: "{{ $labels.name }} was OOM killed. Increase mem_limit or reduce load."

      # ── AI Runtime ────────────────────────────────────────────────
      - alert: OllamaInferenceTimeout
        expr: |
          increase(ai_inference_timeout_total[5m]) > 0
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: "Ollama inference timeout"
          description: "{{ $value }} timeouts in last 5min. Check model load and memory."

      # ── Healing Events ────────────────────────────────────────────
      - alert: HealingThrottled
        expr: |
          increase(healing_events_total{event_type="throttled"}[10m]) > 0
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "Auto-healing throttled — manual intervention required"
          description: "Worker reached max restart limit. System needs human review."
```

### Prometheus config (`infra/prometheus/prometheus.yml` — adicionar rule_files)
```yaml
global:
  scrape_interval: 30s
  evaluation_interval: 30s

rule_files:
  - /etc/prometheus/rules/*.yml  # carrega slo.yml e qualquer outro

alerting:
  alertmanagers:
    - static_configs:
        - targets: []  # sem alertmanager em dev — alertas visíveis no Grafana UI
```

### Dashboard Grafana para alertas (`infra/grafana/dashboards/slo.json` — esquema mínimo)
```json
{
  "title": "SLO Dashboard",
  "panels": [
    {
      "title": "p95 Latency",
      "type": "stat",
      "targets": [{
        "expr": "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))",
        "legendFormat": "p95"
      }],
      "thresholds": {"steps": [{"color": "green"}, {"color": "red", "value": 2}]}
    },
    {
      "title": "Queue Depth",
      "type": "stat",
      "targets": [{
        "expr": "sqs_approximate_number_of_messages_visible",
        "legendFormat": "messages"
      }],
      "thresholds": {"steps": [{"color": "green"}, {"color": "yellow", "value": 50}, {"color": "red", "value": 100}]}
    }
  ]
}
```

## Métricas que precisam existir para os alertas funcionarem

| Métrica | Onde definir | Skill relacionada |
|---|---|---|
| `http_request_duration_seconds_bucket` | Go API service | `observability:go-metrics` |
| `sqs_approximate_number_of_messages_visible` | OTEL Collector scrape SQS | `otel:collector-config` |
| `worker_last_heartbeat_timestamp_seconds` | Go worker heartbeat | `autohealing:heartbeat-tracker` |
| `container_memory_usage_bytes` | cAdvisor (adicionar ao docker-compose) | - |
| `healing_events_total` | Watchdog | `autohealing:worker-watchdog` |
| `ai_inference_timeout_total` | AI runtime | `python-ai:new-inference-endpoint` |

## Checklist pós-geração
- [ ] `slo.yml` em `/etc/prometheus/rules/` no container Prometheus
- [ ] `rule_files` adicionado ao `prometheus.yml`
- [ ] Prometheus recarregou: `curl -X POST http://localhost:9090/-/reload`
- [ ] Alertas visíveis em `http://localhost:9090/alerts`
- [ ] Grafana data source Prometheus apontando para `http://prometheus:9090`
- [ ] Testar alerta: parar go-worker e aguardar `WorkerHeartbeatMissing` disparar
