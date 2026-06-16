# Port Standardization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mover o servidor Go de `:8080` para `:8082`, eliminar conflito com Apache local e padronizar todas as portas do projeto em um único `.env`.

**Architecture:** O Apache local ocupa `0.0.0.0:8080` antes do Go, causando 404 no Prometheus (que scrapa via `host.docker.internal` → IPv4). A solução é mover o Go para `:8082` (sem conflito) e criar um `.env` na raiz com todas as portas, referenciado pelo `docker-compose.yml` de observabilidade e pelo `prometheus.yaml`.

**Tech Stack:** Go `net/http`, variável de ambiente `PORT`, arquivo `.env`, Docker Compose, Prometheus scrape config.

---

## Mapa de arquivos

| Arquivo | Ação | Responsabilidade |
|---|---|---|
| `.env` | Criar | Fonte única de verdade para todas as portas |
| `services/api/cmd/server/main.go` | Modificar | Ler `PORT` do env (default `8082`) |
| `infra/observability/prometheus/prometheus.yaml` | Modificar | Scrape `go-api` em `:8082` |
| `infra/observability/docker-compose.yml` | Modificar | Referência de documentação + `env_file` se aplicável |

**Portas padronizadas:**
```
API Go       → 8082
AI Runtime   → 8001
Grafana      → 3000
Prometheus   → 9090
Tempo        → 3200
OTEL gRPC    → 4317
OTEL HTTP    → 4318
Loki         → 3100
```

---

### Task 1: Criar `.env` com todas as portas

**Files:**
- Create: `.env` (raiz do projeto `TraceRuntime/`)

- [ ] **Step 1: Criar o arquivo `.env`**

```bash
# TraceRuntime — Port Registry
# Fonte única de verdade para todas as portas do projeto.
# Referenciado por: docker-compose.yml, prometheus.yaml, README.

# Go API
GO_API_PORT=8082

# AI Runtime (Python/FastAPI)
AI_RUNTIME_PORT=8001

# Observability stack
GRAFANA_PORT=3000
PROMETHEUS_PORT=9090
TEMPO_PORT=3200
LOKI_PORT=3100

# OTEL Collector
OTEL_GRPC_PORT=4317
OTEL_HTTP_PORT=4318
OTEL_COLLECTOR_METRICS_PORT=8889
```

Salva em `.env` (raiz do projeto).

- [ ] **Step 2: Verificar que o arquivo existe**

```bash
cat .env
```

Esperado: conteúdo acima sem erros.

---

### Task 2: Mudar o servidor Go para porta 8082

**Files:**
- Modify: `services/api/cmd/server/main.go`

- [ ] **Step 1: Atualizar a função `main` para ler `PORT` do env**

Em `services/api/cmd/server/main.go`, substituir a linha:

```go
Addr: ":8080",
```

Por:

```go
Addr: ":" + envString("PORT", "8082"),
```

E adicionar a função helper logo após `envInt`:

```go
func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

O arquivo completo da função `main` fica com:

```go
srv := &http.Server{
    Addr:         ":" + envString("PORT", "8082"),
    Handler:      router,
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 0,
    IdleTimeout:  120 * time.Second,
}

slog.Info("api server starting", "addr", ":"+envString("PORT", "8082"), "queue_capacity", queueCapacity, "ai_runtime_url", aiRuntimeURL)
```

- [ ] **Step 2: Buildar para verificar que compila**

```bash
cd services/api && go build ./...
```

Esperado: sem erros de compilação.

- [ ] **Step 3: Subir o servidor e confirmar porta correta**

```bash
cd services/api && go run ./cmd/server/main.go
```

Em outro terminal:

```bash
curl -s http://127.0.0.1:8082/health
```

Esperado: `{"status":"ok"}` ou similar.

```bash
curl -s http://127.0.0.1:8082/metrics | grep traceruntime_queue_depth
```

Esperado: `traceruntime_queue_depth 0`.

---

### Task 3: Atualizar Prometheus para scraper `:8082`

**Files:**
- Modify: `infra/observability/prometheus/prometheus.yaml`

- [ ] **Step 1: Atualizar target do `go-api`**

Substituir:

```yaml
  - job_name: go-api
    static_configs:
      - targets: ['host.docker.internal:8080']
    metrics_path: /metrics
```

Por:

```yaml
  - job_name: go-api
    static_configs:
      - targets: ['host.docker.internal:8082']
    metrics_path: /metrics
```

- [ ] **Step 2: Recarregar Prometheus**

Com o stack de observabilidade rodando (`docker compose up -d` em `infra/observability/`):

```bash
curl -X POST http://localhost:9090/-/reload
```

Esperado: HTTP 200 (sem output).

- [ ] **Step 3: Verificar target UP no Prometheus**

Abrir `http://localhost:9090/targets` no browser.

Esperado: job `go-api` com status **UP** e endpoint `host.docker.internal:8082`.

- [ ] **Step 4: Verificar métrica no Prometheus**

```
http://localhost:9090/graph?g0.expr=traceruntime_queue_depth
```

Esperado: valor `0` (ou o valor atual da fila).

---

### Task 4: Validar dashboard no Grafana

**Files:**
- Nenhum arquivo modificado — validação operacional.

- [ ] **Step 1: Abrir dashboard "Queue & Worker"**

Acesse `http://localhost:3000` → Dashboards → "Queue & Worker".

- [ ] **Step 2: Disparar uma tarefa para gerar métricas**

```bash
curl -s -X POST http://localhost:8082/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "teste porta 8082"}'
```

Esperado: resposta JSON com `task_id`.

- [ ] **Step 3: Confirmar painéis com dados**

No dashboard "Queue & Worker", verificar:
- **Queue Depth** — mostra valor numérico
- **Enqueued Total** — incrementou (≥ 1)
- **Throughput** — linha visível no gráfico de série temporal

Se todos aparecerem com dados: Task 4 concluída, Task 11 da Phase 4 completa.

---

## Self-Review

**Spec coverage:**
- [x] Go API muda para 8082
- [x] Prometheus scrape atualizado
- [x] `.env` criado com todas as portas padronizadas
- [x] Validação no Grafana
- [ ] `docker-compose.yml` principal — já está vazio (`services: {}`), será populado na Phase 5. Não há nada a alterar agora.

**Placeholder scan:** Nenhum TBD, nenhum "implemente depois". Todos os blocos de código são completos.

**Type consistency:** Apenas configuração e uma função helper Go — sem tipos a checar.
