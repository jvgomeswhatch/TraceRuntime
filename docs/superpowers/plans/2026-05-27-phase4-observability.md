# Phase 4 — Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adicionar OTEL Collector, Prometheus, Grafana, Tempo e Loki ao stack — tornando traces e métricas visíveis — e trocar exporters stdout/console por OTLP gRPC nos serviços Go e Python, com logs fully trace-correlated.

**Architecture:** Stack de observabilidade vive em `infra/observability/` como subsystem independente do runtime (Go + Python ainda rodam fora do Docker). O OTEL Collector recebe spans via OTLP gRPC (4317) e encaminha para o Tempo. Prometheus faz scrape do collector. Loki entra no stack mas sem ingestão nesta fase (placeholder-only — Phase 5 traz logs via Docker log driver). Grafana é o único frontend — datasources e dashboards pré-provisionados via arquivo JSON no repo.

**Tech Stack:** Docker Compose · otel/opentelemetry-collector-contrib:0.104.0 · prom/prometheus:v2.54.1 · grafana/grafana:11.1.0 · grafana/tempo:2.5.0 · grafana/loki:3.0.0 · go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc · opentelemetry-exporter-otlp-proto-grpc==1.33.1

---

## Decisões arquiteturais fixadas

- Stack de observabilidade em `infra/observability/` — não na raiz
- `docker-compose.yml` da raiz fica vazio até Phase 5
- Versões congeladas em `version-lock.yaml` — nunca `latest`, nunca versões independentes
- Collector 0.104.0 alinhado com SDKs Go v1.43.0 e Python 1.33.1
- Configuração via env var `OTEL_EXPORTER_OTLP_ENDPOINT` (padrão OTEL spec) — sem hardcode
  - Default no código: `localhost:4317` (run local fora do Docker)
  - Em Docker (Phase 5): `otel-collector:4317` (docker network) — sem mudança de código
- `task_id` e `trace_id` NUNCA viram label Prometheus — apenas span attribute / log field
- Loki = placeholder-only nesta fase: datasource configurado, sem ingestão, sem Promtail
- Todo log line DEVE ter `trace_id` + `span_id` + `task_id` — requisito estrutural via helpers, não inline spread
- Metric naming padronizado: `ai_request_duration_seconds` (único, sem `inference_duration_seconds` duplicado)
- Prometheus scrape de host services via `host.docker.internal` (Docker Desktop Windows/Mac) — documentado como limitação Linux
- Dashboards pré-provisionados via JSON no repo — reproducibilidade obrigatória

## Limites de memória (definidos agora, não na Phase 5)

```
otel-collector:  384m   (↑ de 256m — burst safety para batch + memory_limiter)
prometheus:      512m
tempo:           512m
loki:            512m
grafana:         256m
total:          ~2.17GB
```

## Estrutura de arquivos

```
infra/
  observability/
    docker-compose.yml
    version-lock.yaml
    otel-collector/
      config.yaml
    prometheus/
      prometheus.yaml
    tempo/
      tempo.yaml
    loki/
      loki.yaml
    grafana/
      provisioning/
        datasources/
          datasources.yaml
        dashboards/
          dashboards.yaml
      dashboards/
        runtime-overview.json
        ai-pipeline.json
        queue-worker.json
```

## Sequência de tasks

```
— INFRA —
Task 1: version-lock.yaml + estrutura de diretórios
Task 2: Configs de cada serviço (otel-collector, prometheus, tempo, loki, grafana)
Task 3: docker-compose.yml em infra/observability/
Task 4: Stack up — validar que todos os 5 serviços sobem

— PIPELINE SANITY —
Task 5: Smoke test do collector com span sintético (antes de tocar nos apps)

— INSTRUMENTATION —
Task 6: Corrigir logs Go — helper logWithTrace + span_id em todo log
Task 7: Corrigir logs Python — helper trace_fields() + trace_id/span_id em todo log

— EXPORTERS —
Task 8: Go exporter stdout → OTLP gRPC
Task 9: Python exporter Console → OTLP gRPC

— VALIDATION —
Task 10: Trace end-to-end Go → Worker → Python visível no Tempo
Task 11: Prometheus métricas + Grafana dashboards
```

**Regra absoluta:** após cada task, PARAR. O usuário testa. Só avançar com confirmação explícita.

---

## Task 1: version-lock.yaml + estrutura de diretórios

**Files:**
- Criar: `infra/observability/version-lock.yaml`
- Criar (diretórios): `infra/observability/otel-collector/`, `prometheus/`, `tempo/`, `loki/`, `grafana/provisioning/datasources/`, `grafana/provisioning/dashboards/`, `grafana/dashboards/`

- [ ] **Step 1: Criar version-lock.yaml**

Criar `infra/observability/version-lock.yaml`:

```yaml
# Stack de observabilidade Phase 4
# Versões congeladas como conjunto validado.
# Nunca atualizar componentes isoladamente — atualizar o stack inteiro como unidade.
# Regra: collector deve ser version-aligned com a era dos SDKs.

otel_collector: "0.104.0"   # alinhado com Go SDK v1.43.0 + Python SDK 1.33.1
prometheus: "v2.54.1"
grafana: "11.1.0"
tempo: "2.5.0"
loki: "3.0.0"

sdk_go: "v1.43.0"
sdk_python: "1.33.1"
```

- [ ] **Step 2: Criar estrutura de diretórios**

```bash
mkdir -p infra/observability/otel-collector
mkdir -p infra/observability/prometheus
mkdir -p infra/observability/tempo
mkdir -p infra/observability/loki
mkdir -p infra/observability/grafana/provisioning/datasources
mkdir -p infra/observability/grafana/provisioning/dashboards
mkdir -p infra/observability/grafana/dashboards
```

- [ ] **Step 3: Verificar estrutura criada**

```bash
find infra/ -type d | sort
```

Resultado esperado:
```
infra/observability
infra/observability/grafana
infra/observability/grafana/dashboards
infra/observability/grafana/provisioning
infra/observability/grafana/provisioning/dashboards
infra/observability/grafana/provisioning/datasources
infra/observability/loki
infra/observability/otel-collector
infra/observability/prometheus
infra/observability/tempo
```

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 2: Configs de cada serviço

**Files:**
- Criar: `infra/observability/otel-collector/config.yaml`
- Criar: `infra/observability/prometheus/prometheus.yaml`
- Criar: `infra/observability/tempo/tempo.yaml`
- Criar: `infra/observability/loki/loki.yaml`
- Criar: `infra/observability/grafana/provisioning/datasources/datasources.yaml`
- Criar: `infra/observability/grafana/provisioning/dashboards/dashboards.yaml`
- Criar: `infra/observability/grafana/dashboards/runtime-overview.json`
- Criar: `infra/observability/grafana/dashboards/ai-pipeline.json`
- Criar: `infra/observability/grafana/dashboards/queue-worker.json`

- [ ] **Step 1: Config do OTEL Collector**

Criar `infra/observability/otel-collector/config.yaml`:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  # memory_limiter DEVE vir antes do batch no pipeline — limita memória antes de acumular.
  memory_limiter:
    check_interval: 1s
    limit_mib: 320
    spike_limit_mib: 64
  batch:
    timeout: 1s
    send_batch_size: 512

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
  prometheus:
    endpoint: "0.0.0.0:8889"
    namespace: traceruntime

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus]
```

- [ ] **Step 2: Config do Prometheus**

Criar `infra/observability/prometheus/prometheus.yaml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ['otel-collector:8889']

  # Nota: host.docker.internal funciona em Docker Desktop (Windows/Mac).
  # Em Linux puro, substituir por 172.17.0.1 ou IP da interface docker0.
  - job_name: ai-runtime
    static_configs:
      - targets: ['host.docker.internal:8001']
    metrics_path: /metrics

  - job_name: go-api
    static_configs:
      - targets: ['host.docker.internal:8080']
    metrics_path: /metrics
```

- [ ] **Step 3: Config do Tempo**

Criar `infra/observability/tempo/tempo.yaml`:

```yaml
stream_over_http_enabled: true

server:
  http_listen_port: 3200
  grpc_listen_port: 9095

# Tempo 2.5.x aceita spans diretamente via OTLP receiver no distributor.
# O Collector envia para tempo:4317 — esse bloco configura o receiver desse lado.
distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: "0.0.0.0:4317"
        http:
          endpoint: "0.0.0.0:4318"

ingester:
  max_block_duration: 5m

storage:
  trace:
    backend: local
    local:
      path: /var/tempo/blocks
    wal:
      path: /var/tempo/wal

compactor:
  compaction:
    block_retention: 1h

query_frontend:
  search:
    duration_slo: 5s
    throughput_bytes_slo: 1.073741824e+09
  trace_by_id:
    duration_slo: 5s
```

Nota: `distributor.receivers.otlp` é o schema válido no Tempo 2.x — o Tempo embute o receiver OTLP diretamente, diferente do Collector que usa o bloco `receivers:` de nível superior. O path de storage usa `/var/tempo` (não `/tmp`) para evitar que o container descarte dados em reinicializações.

- [ ] **Step 4: Config do Loki**

Loki entra como **placeholder-only** nesta fase: sem ingestão, sem Promtail, sem pipeline de logs. O datasource é configurado no Grafana para estar pronto na Phase 5.

Criar `infra/observability/loki/loki.yaml`:

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: 24h
```

- [ ] **Step 5: Datasources do Grafana**

Criar `infra/observability/grafana/provisioning/datasources/datasources.yaml`:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  - name: Tempo
    type: tempo
    uid: tempo
    access: proxy
    url: http://tempo:3200
    editable: false
    jsonData:
      # tracesToLogsV2 desabilitado nesta fase: Loki não ingere logs ainda.
      # Habilitar na Phase 5 quando logs estiverem disponíveis no Loki.
      # tracesToLogsV2:
      #   datasourceUid: loki
      serviceMap:
        datasourceUid: prometheus
      nodeGraph:
        enabled: true

  - name: Loki
    type: loki
    uid: loki
    access: proxy
    url: http://loki:3100
    editable: false
    # Placeholder-only nesta fase: sem ingestão, sem Promtail.
    # Datasource configurado para Phase 5. Não habilitar tracesToLogs no Tempo enquanto não houver dados.
```

- [ ] **Step 6: Provisioning de dashboards**

Criar `infra/observability/grafana/provisioning/dashboards/dashboards.yaml`:

```yaml
apiVersion: 1

providers:
  - name: TraceRuntime
    orgId: 1
    folder: TraceRuntime
    folderUid: traceruntime
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /etc/grafana/dashboards
```

- [ ] **Step 7: Dashboard Runtime Overview**

Métricas usadas: `ai_requests_total`, `ai_failures_total`, `ai_request_duration_seconds` (único, padronizado).

Criar `infra/observability/grafana/dashboards/runtime-overview.json`:

```json
{
  "title": "Runtime Overview",
  "uid": "runtime-overview",
  "version": 1,
  "schemaVersion": 39,
  "refresh": "5s",
  "time": { "from": "now-30m", "to": "now" },
  "panels": [
    {
      "id": 1,
      "title": "Request Rate (req/min)",
      "type": "timeseries",
      "gridPos": { "x": 0, "y": 0, "w": 12, "h": 6 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "rate(traceruntime_ai_requests_total[1m]) * 60",
          "legendFormat": "{{execution_status}}"
        }
      ]
    },
    {
      "id": 2,
      "title": "Error Rate",
      "type": "timeseries",
      "gridPos": { "x": 12, "y": 0, "w": 12, "h": 6 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "rate(traceruntime_ai_failures_total[1m])",
          "legendFormat": "{{reason}}"
        }
      ]
    },
    {
      "id": 3,
      "title": "Latency p50 (s)",
      "type": "stat",
      "gridPos": { "x": 0, "y": 6, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "histogram_quantile(0.50, rate(traceruntime_ai_request_duration_seconds_bucket[5m]))",
          "legendFormat": "p50"
        }
      ]
    },
    {
      "id": 4,
      "title": "Latency p95 (s)",
      "type": "stat",
      "gridPos": { "x": 8, "y": 6, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "histogram_quantile(0.95, rate(traceruntime_ai_request_duration_seconds_bucket[5m]))",
          "legendFormat": "p95"
        }
      ]
    },
    {
      "id": 5,
      "title": "Execution Status Breakdown",
      "type": "piechart",
      "gridPos": { "x": 16, "y": 6, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (execution_status) (traceruntime_ai_requests_total)",
          "legendFormat": "{{execution_status}}"
        }
      ]
    }
  ]
}
```

Nota: o OTEL Collector adiciona o prefixo `traceruntime_` (definido em `namespace: traceruntime` no config do collector) às métricas recebidas via OTLP. Métricas scrapeadas diretamente dos serviços mantêm seus nomes originais (`ai_requests_total`, etc.). Os dashboards de Task 11 validarão qual prefixo está ativo.

- [ ] **Step 8: Dashboard AI Pipeline**

Criar `infra/observability/grafana/dashboards/ai-pipeline.json`:

```json
{
  "title": "AI Pipeline",
  "uid": "ai-pipeline",
  "version": 1,
  "schemaVersion": 39,
  "refresh": "5s",
  "time": { "from": "now-30m", "to": "now" },
  "panels": [
    {
      "id": 1,
      "title": "Inference Duration p50/p95 (s)",
      "type": "timeseries",
      "gridPos": { "x": 0, "y": 0, "w": 24, "h": 8 },
      "fieldConfig": {
        "defaults": {
          "custom": { "fillOpacity": 10 }
        }
      },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "histogram_quantile(0.50, rate(ai_request_duration_seconds_bucket[5m]))",
          "legendFormat": "p50"
        },
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "histogram_quantile(0.95, rate(ai_request_duration_seconds_bucket[5m]))",
          "legendFormat": "p95"
        }
      ]
    },
    {
      "id": 2,
      "title": "Model Usage",
      "type": "timeseries",
      "gridPos": { "x": 0, "y": 8, "w": 12, "h": 6 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (task_type) (rate(ai_requests_total[1m]))",
          "legendFormat": "{{task_type}}"
        }
      ]
    },
    {
      "id": 3,
      "title": "Validation Status",
      "type": "timeseries",
      "gridPos": { "x": 12, "y": 8, "w": 12, "h": 6 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (validation_status) (rate(ai_validation_status_total[1m]))",
          "legendFormat": "{{validation_status}}"
        }
      ]
    }
  ]
}
```

- [ ] **Step 9: Dashboard Queue + Worker**

Criar `infra/observability/grafana/dashboards/queue-worker.json`:

```json
{
  "title": "Queue & Worker",
  "uid": "queue-worker",
  "version": 1,
  "schemaVersion": 39,
  "refresh": "5s",
  "time": { "from": "now-30m", "to": "now" },
  "panels": [
    {
      "id": 1,
      "title": "Tasks Total",
      "type": "stat",
      "gridPos": { "x": 0, "y": 0, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum(ai_requests_total)",
          "legendFormat": "Total"
        }
      ]
    },
    {
      "id": 2,
      "title": "Tasks Completed",
      "type": "stat",
      "gridPos": { "x": 8, "y": 0, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum(ai_requests_total{execution_status=\"completed\"})",
          "legendFormat": "Completed"
        }
      ]
    },
    {
      "id": 3,
      "title": "Tasks Failed",
      "type": "stat",
      "gridPos": { "x": 16, "y": 0, "w": 8, "h": 4 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum(ai_failures_total)",
          "legendFormat": "Failed"
        }
      ]
    },
    {
      "id": 4,
      "title": "Task Lifecycle (rate/min)",
      "type": "timeseries",
      "gridPos": { "x": 0, "y": 4, "w": 24, "h": 8 },
      "targets": [
        {
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "rate(ai_requests_total[1m]) * 60",
          "legendFormat": "{{execution_status}} / {{task_type}}"
        }
      ]
    }
  ]
}
```

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 3: docker-compose.yml em infra/observability/

**Files:**
- Criar: `infra/observability/docker-compose.yml`

- [ ] **Step 1: Criar docker-compose.yml**

Criar `infra/observability/docker-compose.yml`:

```yaml
version: "3.9"

networks:
  observability:
    driver: bridge

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.104.0
    command: ["--config=/etc/otel-collector/config.yaml"]
    volumes:
      - ./otel-collector/config.yaml:/etc/otel-collector/config.yaml:ro
    ports:
      - "4317:4317"   # OTLP gRPC — apps mandam spans aqui
      - "4318:4318"   # OTLP HTTP
      - "8889:8889"   # Prometheus scrape do collector
      - "13133:13133" # Health check endpoint
    networks: [observability]
    restart: unless-stopped
    mem_limit: 384m
    cpus: "0.5"

  prometheus:
    image: prom/prometheus:v2.54.1
    command:
      - "--config.file=/etc/prometheus/prometheus.yaml"
      - "--storage.tsdb.retention.time=2h"
      - "--storage.tsdb.retention.size=400MB"
    volumes:
      - ./prometheus/prometheus.yaml:/etc/prometheus/prometheus.yaml:ro
    ports:
      - "9090:9090"
    networks: [observability]
    depends_on: [otel-collector]
    restart: unless-stopped
    mem_limit: 512m
    cpus: "0.5"

  tempo:
    image: grafana/tempo:2.5.0
    command: ["-config.file=/etc/tempo/tempo.yaml"]
    volumes:
      - ./tempo/tempo.yaml:/etc/tempo/tempo.yaml:ro
    ports:
      - "3200:3200"   # HTTP API + query (acesso externo ao host)
    expose:
      - "4317"        # OTLP gRPC — acessível apenas na rede interna Docker (collector → tempo)
      - "4318"        # OTLP HTTP — interno
    networks: [observability]
    restart: unless-stopped
    mem_limit: 512m
    cpus: "0.5"

  loki:
    image: grafana/loki:3.0.0
    command: ["-config.file=/etc/loki/loki.yaml"]
    volumes:
      - ./loki/loki.yaml:/etc/loki/loki.yaml:ro
    ports:
      - "3100:3100"
    networks: [observability]
    restart: unless-stopped
    mem_limit: 512m
    cpus: "0.5"

  grafana:
    image: grafana/grafana:11.1.0
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_DEFAULT_THEME=dark
      - GF_FEATURE_TOGGLES_ENABLE=traceqlEditor
    volumes:
      - ./grafana/provisioning/datasources:/etc/grafana/provisioning/datasources:ro
      - ./grafana/provisioning/dashboards:/etc/grafana/provisioning/dashboards:ro
      - ./grafana/dashboards:/etc/grafana/dashboards:ro
    ports:
      - "3000:3000"
    networks: [observability]
    depends_on: [prometheus, tempo, loki]
    restart: unless-stopped
    mem_limit: 256m
    cpus: "0.5"
```

Nota sobre porta 4317 do Tempo: o collector se comunica com o Tempo via rede interna Docker (`tempo:4317`). A porta não é exposta ao host — apenas o collector acessa. O host acessa o Tempo pela porta 3200 (HTTP query API).

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 4: Stack up — validar que todos os 5 serviços sobem

**Files:** Nenhum arquivo novo — validação operacional.

- [ ] **Step 1: Subir o stack**

```bash
cd infra/observability
docker compose up -d
```

- [ ] **Step 2: Verificar status de todos os containers**

```bash
docker compose ps
```

Resultado esperado: 5 serviços com status `running` (não `restarting`).

- [ ] **Step 3: Health check de cada serviço**

```bash
# OTEL Collector health check
curl -s http://localhost:13133/

# Prometheus
curl -s http://localhost:9090/-/ready

# Tempo
curl -s http://localhost:3200/ready

# Loki
curl -s http://localhost:3100/ready

# Grafana
curl -s http://localhost:3000/api/health
```

Resultado esperado em cada: HTTP 200 com JSON de status.

- [ ] **Step 4: Verificar Grafana no browser**

Abrir `http://localhost:3000`.

Verificar:
1. Grafana carrega sem login (anonymous auth ativo)
2. Em Connections → Data Sources: aparecem Prometheus, Tempo e Loki
3. Em Dashboards → TraceRuntime: aparecem os 3 dashboards (Runtime Overview, AI Pipeline, Queue & Worker)

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 5: Smoke test do collector com span sintético

**Objetivo:** Confirmar que o pipeline OTEL Collector → Tempo funciona **antes** de mexer nos apps. Se falhar aqui, o problema é de infra — não de código.

**Regra:** Só avançar para Task 6 após confirmar que o span sintético aparece no Tempo.

**Files:** Nenhum arquivo novo.

- [ ] **Step 1: Enviar span sintético via OTLP HTTP**

```bash
curl -s -X POST http://localhost:4318/v1/traces \
  -H "Content-Type: application/json" \
  -d '{
    "resourceSpans": [{
      "resource": {
        "attributes": [{
          "key": "service.name",
          "value": {"stringValue": "smoke-test"}
        }]
      },
      "scopeSpans": [{
        "scope": {"name": "smoke-test"},
        "spans": [{
          "traceId": "0af7651916cd43dd8448eb211c80319c",
          "spanId": "b7ad6b7169203331",
          "name": "smoke-test-span",
          "kind": 1,
          "startTimeUnixNano": "1700000000000000000",
          "endTimeUnixNano":   "1700000001000000000",
          "status": {"code": 1}
        }]
      }]
    }]
  }'
```

Resultado esperado: HTTP 200 com `{}`.

- [ ] **Step 2: Verificar que o span chegou no Tempo**

1. Abrir `http://localhost:3000`
2. Ir em Explore → datasource **Tempo**
3. Buscar por TraceID: `0af7651916cd43dd8448eb211c80319c`

Resultado esperado: span `smoke-test-span` visível.

- [ ] **Step 3: Se não aparecer — diagnosticar antes de qualquer outra coisa**

```bash
# Logs do collector — procurar erros de export ou connection refused
docker compose logs otel-collector --tail=50

# Logs do tempo — procurar erros de ingestion
docker compose logs tempo --tail=50
```

Causas comuns:
- `connection refused tempo:4317` → Tempo ainda iniciando, aguardar 10s e tentar novamente
- `tls: no such file` → verificar `insecure: true` no config do collector
- Span enviado mas não aparece → verificar `startTimeUnixNano` (Tempo descarta spans muito antigos se `block_retention` for pequeno)

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 6: Corrigir logs Go — helper logWithTrace + span_id

**Objetivo:** Todo log do Go API/worker deve conter `trace_id`, `span_id` e `task_id`. Implementar via helper centralizado — não inline spread em cada log.

**Files:**
- Criar: `services/api/internal/telemetry/log.go`
- Modificar: `services/api/internal/worker/worker.go`
- Modificar: `services/api/internal/http/task.go`

- [ ] **Step 1: Criar helper de log trace-correlated**

Criar `services/api/internal/telemetry/log.go`:

```go
package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// LogAttrs extrai trace_id e span_id do context OTEL ativo e retorna como slog.Attr.
// Retorna slice vazio se não houver span ativo — nunca falha.
func LogAttrs(ctx context.Context) []any {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsValid() {
		return nil
	}
	return []any{
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	}
}

// Info loga com trace_id e span_id extraídos do context.
func Info(ctx context.Context, msg string, args ...any) {
	slog.Info(msg, append(LogAttrs(ctx), args...)...)
}

// Error loga com trace_id e span_id extraídos do context.
func Error(ctx context.Context, msg string, args ...any) {
	slog.Error(msg, append(LogAttrs(ctx), args...)...)
}
```

- [ ] **Step 2: Atualizar worker.go para usar o helper**

No `services/api/internal/worker/worker.go`:

1. Adicionar import `"github.com/runtime-platform/services/api/internal/telemetry"` (já existe como package, só adicionar o path)
2. Substituir todos os `slog.Info(...)` e `slog.Error(...)` por `telemetry.Info(parentCtx, ...)` e `telemetry.Error(parentCtx, ...)`
3. Remover as variáveis `traceID` e a extração manual do span (o helper faz isso via context)

Resultado em `worker.go` após mudança:

```go
func (w *Worker) process(ctx context.Context, t queue.Task) {
	carrier := propagation.MapCarrier{"traceparent": t.Traceparent}
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	_, span := workerTracer.Start(parentCtx, "task.process")
	defer span.End()

	spanCtx := span.SpanContext()
	traceparent := "00-" + spanCtx.TraceID().String() + "-" + spanCtx.SpanID().String() + "-01"

	w.publish(sseEvent{
		EventType:   "task.processing",
		TraceID:     spanCtx.TraceID().String(),
		Traceparent: traceparent,
		TaskID:      t.ID,
		Source:      "worker",
	})

	deadline := time.Now().Add(120 * time.Second)
	inferCtx, cancel := context.WithDeadline(parentCtx, deadline)
	defer cancel()

	resp, err := w.aiClient.Infer(inferCtx, ai.InferRequest{
		TaskID:         t.ID,
		Input:          t.Payload,
		DeadlineUnixMs: deadline.UnixMilli(),
		Traceparent:    traceparent,
	})

	if err != nil {
		reason := "ai_runtime_error"
		if inferCtx.Err() == context.DeadlineExceeded {
			reason = "timeout"
		}
		telemetry.Error(parentCtx, "task failed",
			"task_id", t.ID,
			"error", err,
			"reason", reason,
		)
		w.publish(sseEvent{
			EventType:   "task.failed",
			TraceID:     spanCtx.TraceID().String(),
			Traceparent: traceparent,
			TaskID:      t.ID,
			Source:      "worker",
			ErrorReason: reason,
		})
		return
	}

	if resp.ExecutionStatus == "failed" {
		telemetry.Error(parentCtx, "task failed — runtime returned failed status",
			"task_id", t.ID,
			"validation_status", resp.ValidationStatus,
		)
		w.publish(sseEvent{
			EventType:        "task.failed",
			TraceID:          spanCtx.TraceID().String(),
			Traceparent:      traceparent,
			TaskID:           t.ID,
			Source:           "worker",
			ErrorReason:      "ai_runtime_failed",
			ExecutionStatus:  resp.ExecutionStatus,
			ValidationStatus: resp.ValidationStatus,
		})
		return
	}

	telemetry.Info(parentCtx, "task completed",
		"task_id", t.ID,
		"model", resp.ExecutionProfile.Model,
		"execution_status", resp.ExecutionStatus,
		"inference_duration_ms", resp.InferenceDurationMs,
	)

	w.publish(sseEvent{
		EventType:           "task.completed",
		TraceID:             spanCtx.TraceID().String(),
		Traceparent:         traceparent,
		TaskID:              t.ID,
		Source:              "worker",
		Output:              resp.Output,
		Model:               resp.ExecutionProfile.Model,
		ExecutionStatus:     resp.ExecutionStatus,
		ValidationStatus:    resp.ValidationStatus,
		InferenceDurationMs: resp.InferenceDurationMs,
	})
}
```

- [ ] **Step 3: Atualizar task.go**

Abrir `services/api/internal/http/task.go`. Verificar se os logs de criação de task passam o `ctx` com span ativo. Se sim, substituir `slog.Info(...)` por `telemetry.Info(ctx, ...)` para garantir `trace_id` + `span_id`.

- [ ] **Step 4: Build**

```bash
cd services/api
go build ./...
```

Resultado esperado: sem erros.

- [ ] **Step 5: Testar — verificar logs com trace_id e span_id**

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/server/main.go
```

Em outro terminal:
```bash
curl -s -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "test log correlation"}'
```

Verificar no stdout do servidor que os logs JSON contêm os campos `trace_id` e `span_id`:
```json
{"time":"...","level":"INFO","msg":"task completed","trace_id":"abc...","span_id":"def...","task_id":"..."}
```

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 7: Corrigir logs Python — helper trace_fields() + trace_id/span_id

**Objetivo:** Todo log do AI Runtime dentro de span ativo deve conter `trace_id` e `span_id`. Implementar via helper com fallback explícito — não spread inline.

**Files:**
- Modificar: `ai-runtime/main.py`

- [ ] **Step 1: Adicionar helper trace_fields no topo de main.py**

No arquivo `ai-runtime/main.py`, logo após os imports existentes, adicionar:

```python
from opentelemetry.trace import get_current_span


def _trace_fields() -> dict:
    span = get_current_span()
    if not span.is_recording():
        return {}
    ctx = span.get_span_context()
    if not ctx.is_valid:
        return {}
    return {
        "trace_id": format(ctx.trace_id, "032x"),
        "span_id": format(ctx.span_id, "016x"),
    }
```

Nota: `span.is_recording()` é o guard correto — retorna False fora de span ativo, evitando trace_id silenciosamente ausente.

- [ ] **Step 2: Mover log de "infer request received" para dentro do span**

Em `main.py`, no endpoint `/infer`, o log `"infer request received"` está **fora** do `with _infer_tracer.start_as_current_span(...)`. Mover para dentro, logo após `span.set_attribute("task_id", task_id)`:

```python
with _infer_tracer.start_as_current_span("ai.infer") as span:
    span.set_attribute("task_id", task_id)

    log.info("infer request received", extra={"task_id": task_id, **_trace_fields()})

    # ... resto do código
```

- [ ] **Step 3: Adicionar _trace_fields() em todos os log.info/log.error dentro do span**

Localizar cada chamada dentro do bloco `with _infer_tracer.start_as_current_span("ai.infer")`:

```python
# antes
log.info("infer request completed", extra={
    "task_id": task_id,
    "execution_status": execution_status,
    ...
})

# depois
log.info("infer request completed", extra={
    "task_id": task_id,
    "execution_status": execution_status,
    ...,
    **_trace_fields(),
})
```

Aplicar o mesmo em `log.error(...)`.

- [ ] **Step 4: Testar — verificar logs com trace_id e span_id**

```bash
cd ai-runtime
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 .venv/Scripts/python -m uvicorn main:app --port 8001
```

Disparar uma inferência (via Go API ou curl direto no `/infer`) e verificar que os logs contêm `trace_id` e `span_id`:

```json
{"time": "...", "level": "INFO", "msg": "infer request completed", "name": "...", "task_id": "...", "trace_id": "abc...", "span_id": "def..."}
```

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 8: Go exporter stdout → OTLP gRPC

**Files:**
- Modificar: `services/api/internal/telemetry/otel.go`
- Modificar: `services/api/go.mod`

- [ ] **Step 1: Adicionar dependência OTLP**

```bash
cd services/api
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0
```

Resultado esperado: `go.mod` e `go.sum` atualizados sem erros. Não é necessário `go get google.golang.org/grpc` separado — o SDK gerencia a dependência interna.

- [ ] **Step 2: Substituir otel.go**

Substituir o conteúdo completo de `services/api/internal/telemetry/otel.go`:

```go
package telemetry

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init configura TracerProvider com OTLP gRPC exporter.
// Endpoint via OTEL_EXPORTER_OTLP_ENDPOINT — default localhost:4317 (run local).
// Em Docker (Phase 5), setar OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 via env, sem mudança de código.
func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	// Usa opções nativas do SDK — sem gerenciar conn gRPC manualmente.
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(1*time.Second)),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}
```

- [ ] **Step 3: Build**

```bash
cd services/api
go build ./...
```

Resultado esperado: sem erros.

- [ ] **Step 4: Rodar e verificar spans no Tempo**

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/server/main.go
```

Em outro terminal:
```bash
curl -s -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "test otlp export"}'
```

1. Abrir `http://localhost:3000` → Explore → Tempo
2. Buscar por `{service.name="eventdrive-api"}`
3. Go não deve mais printar spans no stdout

Resultado esperado: spans visíveis no Tempo com service name `eventdrive-api`.

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 9: Python exporter Console → OTLP gRPC

**Files:**
- Modificar: `ai-runtime/requirements.txt`
- Modificar: `ai-runtime/telemetry.py`

- [ ] **Step 1: Adicionar dependência OTLP**

Substituir conteúdo de `ai-runtime/requirements.txt`:

```
fastapi==0.115.12
uvicorn[standard]==0.34.2
langgraph==0.4.5
opentelemetry-api==1.33.1
opentelemetry-sdk==1.33.1
opentelemetry-exporter-otlp-proto-grpc==1.33.1
prometheus-client==0.22.1
httpx==0.28.1
```

Instalar:
```bash
cd ai-runtime
.venv/Scripts/activate
pip install opentelemetry-exporter-otlp-proto-grpc==1.33.1
```

Resultado esperado: instalação sem erros.

- [ ] **Step 2: Substituir telemetry.py**

Substituir conteúdo completo de `ai-runtime/telemetry.py`:

```python
import os
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.propagate import set_global_textmap
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter


def init_telemetry(service_name: str = "traceruntime-ai-runtime") -> TracerProvider:
    # Default localhost:4317 para run local fora do Docker.
    # Em Docker (Phase 5): OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")

    resource = Resource.create({
        "service.name": service_name,
        "service.version": "0.1.0",
    })

    exporter = OTLPSpanExporter(endpoint=endpoint, insecure=True)

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)
    set_global_textmap(TraceContextTextMapPropagator())
    return provider


def get_tracer(name: str):
    return trace.get_tracer(name)
```

- [ ] **Step 3: Rodar e verificar**

```bash
cd ai-runtime
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 .venv/Scripts/python -m uvicorn main:app --port 8001
```

Verificar health:
```bash
curl http://localhost:8001/health
```

Python não deve mais printar spans no console. Spans devem aparecer no Tempo com service name `traceruntime-ai-runtime`.

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 10: Trace end-to-end Go → Worker → Python visível no Tempo

**Objetivo:** Confirmar que o mesmo `trace_id` atravessa Go API → Worker → AI Runtime como trace único no Tempo com 3+ spans encadeados.

**Files:** Nenhum arquivo novo — validação operacional pura.

- [ ] **Step 1: Iniciar todos os serviços**

Terminal 1 — Observability stack:
```bash
cd infra/observability
docker compose ps   # verificar que está rodando
```

Terminal 2 — Go API + Worker:
```bash
cd services/api
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/server/main.go
```

Terminal 3 — AI Runtime:
```bash
cd ai-runtime
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 .venv/Scripts/python -m uvicorn main:app --port 8001
```

- [ ] **Step 2: Criar task e capturar o trace_id**

```bash
curl -s -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "end-to-end trace validation"}'
```

Anotar o `trace_id` da resposta JSON.

- [ ] **Step 3: Verificar trace no Tempo**

1. Abrir `http://localhost:3000`
2. Explore → datasource **Tempo**
3. Buscar pelo `trace_id` anotado

Resultado esperado: trace único com **pelo menos 3 spans encadeados**:
- `eventdrive-api` — span do handler HTTP
- `eventdrive-api/worker` — span `task.process`
- `traceruntime-ai-runtime` — span `ai.infer`

Todos com o mesmo `trace_id` e exibindo hierarquia parent→child correta.

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Task 11: Prometheus métricas + Grafana dashboards

**Files:** Nenhum arquivo novo — validação operacional.

- [ ] **Step 1: Verificar métricas no Prometheus**

1. Abrir `http://localhost:9090`
2. Executar query: `ai_requests_total`
3. Criar mais tasks e verificar que o contador incrementa
4. Se aparecer com prefixo `traceruntime_`: as métricas chegam via OTEL pipeline. Se sem prefixo: chegam via scrape direto. Ambos são válidos — anotar qual está ativo.

- [ ] **Step 2: Ajustar queries dos dashboards se necessário**

Se as métricas aparecerem com prefixo `traceruntime_` (via OTEL pipeline), os dashboards já usam esse prefixo corretamente (Runtime Overview). Se scrapeadas diretamente (sem prefixo), os dashboards AI Pipeline e Queue & Worker já usam o nome sem prefixo — ambos funcionam.

- [ ] **Step 3: Verificar dashboards no Grafana**

1. Abrir `http://localhost:3000`
2. Dashboards → TraceRuntime → **Runtime Overview** — painéis mostram dados reais (não "No data")
3. Dashboards → TraceRuntime → **AI Pipeline** — inference duration histogram com dados
4. Dashboards → TraceRuntime → **Queue & Worker** — task counts incrementando

- [ ] **Step 4: Verificar datasource Loki**

1. Connections → Data Sources → Loki
2. Clicar em "Save & Test"
3. Resultado esperado: conexão OK (datasource responde, mesmo sem dados ingeridos)

**PARAR — aguardar validação do usuário antes de continuar.**

---

## Definition of Done — Phase 4

- [ ] `infra/observability/version-lock.yaml` existe com versões congeladas
- [ ] Docker Compose sobe 5 serviços sem restart loops
- [ ] Grafana acessível em `http://localhost:3000` com 3 datasources pré-provisionados
- [ ] 3 dashboards visíveis (Runtime Overview, AI Pipeline, Queue & Worker)
- [ ] Span sintético (smoke test Task 5) aparece no Tempo via TraceID
- [ ] Logs Go contêm `trace_id`, `span_id`, `task_id` via helper `telemetry.Info/Error`
- [ ] Logs Python contêm `trace_id`, `span_id`, `task_id` via helper `_trace_fields()`
- [ ] Go não printa mais spans no stdout
- [ ] Python não printa mais spans no console
- [ ] Trace end-to-end (3+ spans encadeados) visível no Tempo com mesmo `trace_id`
- [ ] Métricas `ai_requests_total`, `ai_request_duration_seconds` visíveis no Prometheus
- [ ] `task_id` e `trace_id` não são labels Prometheus — apenas atributos de span/log
- [ ] Memória total dos containers ≤ 2.2GB
- [ ] Loki datasource conectado (sem ingestão — Phase 5)
