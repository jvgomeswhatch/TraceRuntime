# Phase 10 — Features

Baseline, riscos e infraestrutura compartilhada: [phase-10-baseline-and-risks.md](phase-10-baseline-and-risks.md)

---

## 1. Trace Details

### O que resolve

Hoje o usuário vê que uma task demorou 31s mas não sabe onde o tempo foi gasto. O Trace Details mostra o breakdown por estágio.

### Backend

**Novo package:** `services/api/internal/tempo/`

```go
// client.go
package tempo

type Client struct {
    baseURL    string
    httpClient *http.Client
}

func New(baseURL string) *Client {
    return &Client{
        baseURL:    baseURL,
        httpClient: &http.Client{Timeout: 5 * time.Second},
    }
}

func (c *Client) GetTrace(ctx context.Context, traceID string) (*TraceResponse, error) {
    // 1. Validar traceID (hex, 32 chars) antes de fazer HTTP call
    // 2. GET {baseURL}/api/traces/{traceID} com Accept: application/json
    // 3. Se 404 → retornar nil, nil (trace expirou, não é erro)
    // 4. Se timeout/error → retornar nil, err
    // 5. Parse OTLP JSON → TraceResponse
}
```

```go
// types.go
package tempo

type Span struct {
    SpanID            string            `json:"span_id"`
    ParentSpanID      string            `json:"parent_span_id"`
    OperationName     string            `json:"operation_name"`
    ServiceName       string            `json:"service_name"`
    StartTimeUnixNano int64             `json:"start_time_unix_nano"`
    DurationNano      int64             `json:"duration_nano"`
    Status            string            `json:"status"`
    Attributes        map[string]string `json:"attributes,omitempty"`
}

type TraceResponse struct {
    TraceID  string   `json:"trace_id"`
    Spans    []Span   `json:"spans"`
    Services []string `json:"services"`
}
```

**Novo handler:** `services/api/internal/handlers/traces.go`

```go
type TracesHandler struct {
    tempo *tempo.Client
    db    *db.DB
}
```

**Rota:** `GET /api/traces/{traceID}` — protegida por X-Internal-Token

**Lógica:**
1. Extrair traceID do path (chi URL param)
2. Validar formato (hex string, 32 chars)
3. Query paralela: `db.GetTaskByTraceID(ctx, traceID)` + `tempo.GetTrace(ctx, traceID)`
4. Se task não existe no DB → 404
5. Se Tempo indisponível → response com task metadata + `spans: null`
6. Se Tempo retorna dados → incluir spans no response

**Response (sucesso completo):**
```json
{
  "trace_id": "abc123def456...",
  "task": {
    "id": "uuid",
    "status": "completed",
    "created_at": "2024-01-15T14:32:00Z",
    "processing_started_at": "2024-01-15T14:32:00.015Z",
    "completed_at": "2024-01-15T14:32:31.215Z",
    "model": "qwen2.5:3b",
    "prompt_tokens": 45,
    "completion_tokens": 120,
    "tokens_per_second": 3.1
  },
  "spans": [
    {
      "span_id": "abc",
      "parent_span_id": "",
      "operation_name": "POST /tasks",
      "service_name": "traceruntime-api",
      "start_time_unix_nano": 1719500000000000000,
      "duration_nano": 3000000,
      "status": "ok",
      "attributes": {}
    }
  ],
  "services": ["traceruntime-api", "traceruntime-worker", "traceruntime-ai-runtime"],
  "tempo_available": true
}
```

**Response (Tempo indisponível/trace expirado):**
```json
{
  "trace_id": "abc123def456...",
  "task": { "..." },
  "spans": null,
  "services": null,
  "tempo_available": false
}
```

**Nova query DB:**
```go
func (d *DB) GetTaskByTraceID(ctx context.Context, traceID string) (*TaskDetail, error) {
    // SELECT id, trace_id, status, created_at, processing_started_at, completed_at,
    //        COALESCE(artifact_key, ''), COALESCE(error_message, ''),
    //        COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
    //        COALESCE(tokens_per_second, 0), COALESCE(model, '')
    // FROM tasks WHERE trace_id = $1
    // LIMIT 1
    //
    // Usa index idx_tasks_trace_id — O(log n), sem risco de full scan
}
```

### Frontend

**Página:** `frontend/app/traces/[traceId]/page.tsx`

**Waterfall:** Barras horizontais proporcionais à duração, uma por span. Implementação CSS pura:
- Cada span = `<div>` com `width` proporcional à duração relativa ao total
- `margin-left` proporcional ao start_time relativo ao início do trace
- Cor por service_name (hardcoded: api=blue, worker=green, ai-runtime=purple)
- Hover mostra attributes

**Layout:**
```
← Back to Traces

Trace abc123...              Duration: 31.2s    Spans: 7    Services: 3

┌─ Timeline ─────────────────────────────────────────────────────────┐
│ api     POST /tasks      ██ 3ms                                   │
│ api     task.create       █ 1ms                                   │
│ worker  task.process     ███████████████████████████████ 31.2s     │
│   ai    ai.infer          ██████████████████████████████ 31.1s    │
│   ai    graph.classify      █ 2ms                                │
│   ai    graph.generate      ███████████████████████████ 31.0s    │
│   ai    graph.validate      █ 1ms                                │
└────────────────────────────────────────────────────────────────────┘

┌─ Task Info ────────────────────────────────────────────────────────┐
│ Status: completed    Model: qwen2.5:3b                            │
│ Tokens: 45 prompt / 120 completion / 3.1 tok/s                    │
│ Created: 14:32:00    Completed: 14:32:31                          │
└────────────────────────────────────────────────────────────────────┘

[Tempo unavailable? Shows: "Trace data expired (retention: 1h). Task metadata shown from database."]
```

**Navegação:** Na página `/traces`, cada row de trace_id vira link para `/traces/[traceId]`.

### Teste CI

```
# Caso 1: Task existe, Tempo pode não ter spans (mock-ai-runtime no CI)
POST /tasks → wait completed → GET /api/traces/{trace_id}
  ✓ response 200
  ✓ task.id matches
  ✓ task.status == "completed"
  ✓ task.prompt_tokens >= 0
  ✓ tempo_available is boolean

# Caso 2: Trace não existe
GET /api/traces/0000000000000000000000000000dead
  ✓ response 404

# Caso 3: TraceID formato inválido
GET /api/traces/not-a-hex-string
  ✓ response 400
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/http/router.go` | Adicionar rota `GET /api/traces/{traceID}` | Nenhum — adição pura |
| `services/api/internal/db/tasks.go` | Adicionar `GetTaskByTraceID()` | Nenhum — query nova, usa index existente |
| `services/api/cmd/server/main.go` | Adicionar env var `TEMPO_URL` | Nenhum — variável opcional com default |
| `docker-compose.yml` | Adicionar `TEMPO_URL=http://tempo:3200` no API | Nenhum — env var nova |
| `frontend/app/traces/page.tsx` | Adicionar link para `/traces/[traceId]` | Baixo — mudança de texto para `<Link>` |

**Nenhum arquivo existente tem lógica alterada. Todas as mudanças são adições.**

---

## 2. Request Inspector

### O que resolve

Ao abrir uma task, o inspector mostra todos os artefatos daquela execução em um único lugar: trace context, payload, output, artifact, métricas de tokens.

### Backend

**Novo handler:** `services/api/internal/handlers/task_inspect.go`

```go
type TaskInspectHandler struct {
    db    *db.DB
    tempo *tempo.Client   // reutiliza o client criado para Trace Details
    s3    *s3.Client      // NOVO — precisa ser adicionado ao API
}
```

**Rota:** `GET /api/tasks/{taskID}/inspect` — protegida por X-Internal-Token

**Lógica:**
1. Validar taskID (UUID format)
2. `db.GetTask(ctx, taskID)` — busca task por PK (instantâneo)
3. Se task não existe → 404
4. Se `artifact_key` não vazio → S3 GetObject com timeout 3s
5. Se `trace_id` não vazio → `tempo.GetTrace(ctx, traceID)` (reutiliza client)
6. Computar stages a partir dos spans (se disponíveis) ou timestamps do DB (fallback)
7. Retornar tudo agregado

**Stage computation (fallback sem Tempo):**
```go
// Com spans do Tempo:
//   api_duration = span("POST /tasks").duration
//   worker_duration = span("task.process").duration
//   ai_duration = span("ai.infer").duration
//   queue_wait = worker_span.start - api_span.end (estimativa)
//   s3_write = worker_span.end - ai_span.end (estimativa)

// Sem spans (fallback PostgreSQL):
//   total_duration = completed_at - created_at
//   processing_duration = completed_at - processing_started_at
//   queue_wait = processing_started_at - created_at (estimativa)
//   inference_duration = inference_duration_ms (do token metrics, se disponível)
```

**Novo S3 client no API:**
```go
// Em router.go:
s3Cfg, _ := awsconfig.LoadDefaultConfig(context.Background())
s3Client := s3.NewFromConfig(s3Cfg, func(o *s3.Options) {
    o.UsePathStyle = true  // LocalStack requer path-style
})
```

**Nova query DB:**
```go
func (d *DB) GetTask(ctx context.Context, taskID string) (*TaskDetail, error) {
    // SELECT id, trace_id, status, created_at, processing_started_at, completed_at,
    //        COALESCE(artifact_key, ''), COALESCE(error_message, ''),
    //        COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
    //        COALESCE(tokens_per_second, 0), COALESCE(model, '')
    // FROM tasks WHERE id = $1::uuid
    //
    // Busca por PK — O(1), sem risco de performance
}
```

**Response:**
```json
{
  "task": {
    "id": "uuid",
    "trace_id": "abc123",
    "status": "completed",
    "created_at": "...",
    "processing_started_at": "...",
    "completed_at": "...",
    "error_message": "",
    "model": "qwen2.5:3b"
  },
  "tokens": {
    "prompt_tokens": 45,
    "completion_tokens": 120,
    "tokens_per_second": 3.1
  },
  "artifact": {
    "key": "abc123/uuid.json",
    "content": { "output": "...", "model": "qwen2.5:3b" }
  },
  "stages": {
    "total_ms": 31215,
    "queue_wait_ms": 15,
    "processing_ms": 31200,
    "inference_ms": 31100,
    "source": "database"
  },
  "trace": {
    "spans": ["..."],
    "tempo_available": true
  }
}
```

**Response (artifact no S3 indisponível):**
```json
{
  "task": { "..." },
  "tokens": { "..." },
  "artifact": null,
  "stages": { "..." },
  "trace": { "..." }
}
```

### Frontend

**Implementação:** Panel expandível na página `/tasks`, NÃO página separada.

Clicar em uma task na lista expande um detalhe inline:

```
▼ Task uuid-abc...         completed         31.2s         qwen2.5:3b

  ┌─ Trace Context ─────────────────────┐
  │ trace_id: abc123...                 │
  └─────────────────────────────────────┘

  ┌─ Stages ────────────────────────────┐
  │ Queue Wait      15ms    ✓           │
  │ Processing      31.2s   ✓           │
  │ Inference       31.1s   ✓           │
  └─────────────────────────────────────┘

  ┌─ Tokens ────────────────────────────┐
  │ prompt: 45  completion: 120         │
  │ 3.1 tok/s   model: qwen2.5:3b      │
  └─────────────────────────────────────┘

  ┌─ Artifact ──────────────────────────┐
  │ { "output": "Distributed..." }      │
  └─────────────────────────────────────┘

  [View Full Trace →]
```

**Fetch:** Lazy load — só busca `/api/tasks/{taskID}/inspect` quando o usuário clica na row.

### Teste CI

```
POST /tasks → wait completed → GET /api/tasks/{task_id}/inspect
  ✓ response 200
  ✓ task.status == "completed"
  ✓ tokens.prompt_tokens >= 0
  ✓ stages.total_ms > 0
  ✓ artifact is object or null (S3 pode não ter no CI)

# Task não existe
GET /api/tasks/00000000-0000-0000-0000-000000000000/inspect
  ✓ response 404

# UUID inválido
GET /api/tasks/not-a-uuid/inspect
  ✓ response 400
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/http/router.go` | Adicionar rota + S3 client init | Baixo — adição, mas toca no NewRouter |
| `services/api/go.mod` | Adicionar `github.com/aws/aws-sdk-go-v2/service/s3` | Nenhum — dependência já usada no worker |
| `services/api/internal/db/tasks.go` | Adicionar `GetTask()` | Nenhum — função nova |
| `docker-compose.yml` | Adicionar `S3_BUCKET=traceruntime-outputs` no API | Nenhum — env var nova |

**Risco principal:** S3 client initialization no router. Se `awsconfig.LoadDefaultConfig` falhar (AWS env vars ausentes), o API não starta. Mitigação: inicializar S3 client apenas se `S3_BUCKET` env var estiver definida — request inspector funciona sem S3 (retorna `artifact: null`).

---

## 3. Runtime Topology

### O que resolve

Comunica a arquitetura do sistema em segundos com métricas reais de cada componente.

### Backend

**Novo handler:** `services/api/internal/handlers/topology.go`

```go
type TopologyHandler struct {
    db        *db.DB
    sqsClient *sqssdk.Client
    queueURL  string
    dlqURL    string
    aiURL     string
}
```

**Rota:** `GET /api/runtime/topology` — protegida por X-Internal-Token

**Lógica (paralela via errgroup):**
```go
func (h *TopologyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    result := TopologyResponse{Timestamp: time.Now().UTC()}
    var mu sync.Mutex
    g, gCtx := errgroup.WithContext(ctx)

    // 1. API — sempre healthy (self)
    // 2. SQS — GetQueueAttributes (depth, inflight) | Se falhar → "down"
    // 3. Worker — query worker_heartbeats | Se stale → "degraded"
    // 4. AI Runtime — HTTP GET /health timeout 2s | Se timeout → "down"
    // 5. PostgreSQL — pool.Ping() | Se falhar → "down"
    // 6. S3 — HeadBucket timeout 2s | Se falhar → "down"

    g.Wait()
}
```

**Cache server-side:**
```go
var (
    topologyCache     *TopologyResponse
    topologyCacheTime time.Time
    topologyCacheTTL  = 5 * time.Second
    topologyCacheMu   sync.Mutex
)
```
Se cache válido (< 5s) → retorna imediatamente sem fazer health checks.

**Métricas derivadas do PostgreSQL (window 5 min):**
- `db.RuntimeMetrics(ctx, 300)` — **reutiliza query existente**
- Queue depth + inflight: reutiliza `GetQueueAttributes` que o RuntimeMetricsHandler já usa

**Response:**
```json
{
  "timestamp": "2024-01-15T14:35:00Z",
  "nodes": [
    { "name": "api", "status": "healthy", "metrics": { "requests_completed": 142, "error_rate": 2.1, "avg_latency_ms": 3 } },
    { "name": "sqs", "status": "healthy", "metrics": { "queue_depth": 2, "inflight": 1, "dlq_depth": 0 } },
    { "name": "worker", "status": "healthy", "metrics": { "tasks_processed": 140, "tasks_failed": 2, "uptime_seconds": 9240 } },
    { "name": "ai-runtime", "status": "healthy", "metrics": {} },
    { "name": "postgres", "status": "healthy", "metrics": {} },
    { "name": "s3", "status": "healthy", "metrics": {} }
  ]
}
```

### Frontend

**Página:** `frontend/app/topology/page.tsx`

**Layout:** Grid vertical, CSS puro (cards + setas). Sem library de grafos.

```
Runtime Topology                    Updated: 3s ago

  ┌──────────────┐
  │     API      │
  │  ● Healthy   │
  │  142 tasks   │
  │  2.1% error  │
  └──────┬───────┘
         │
  ┌──────▼───────┐
  │     SQS      │
  │  ● Healthy   │
  │  depth: 2    │
  │  dlq: 0      │
  └──────┬───────┘
         │
  ┌──────▼───────┐
  │    Worker    │
  │  ● Healthy   │
  │  140 tasks   │
  │  uptime: 2h  │
  └──────┬───────┘
         │
  ┌──────▼───────┐
  │  AI Runtime  │
  │  ● Healthy   │
  └──────┬───────┘
         │
    ┌────┴────┐
    │         │
 ┌──▼───┐ ┌──▼───┐
 │  S3  │ │  DB  │
 │  ●   │ │  ●   │
 └──────┘ └──────┘
```

**Polling:** 10s interval. Cache server-side de 5s garante no máximo 1 health check a cada 5s.

**Cores:** verde (healthy), amarelo (degraded), vermelho (down).

### Teste CI

```
GET /api/runtime/topology
  ✓ response 200
  ✓ nodes array length == 6
  ✓ each node has name, status
  ✓ api.status == "healthy"
  ✓ sqs node has queue_depth in metrics
  ✓ timestamp is valid ISO 8601
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/http/router.go` | Adicionar rota | Nenhum |
| `docker-compose.yml` | Adicionar `AI_RUNTIME_URL` no API (já existe!) | Nenhum |

**Reuso de código existente:**
- `db.ListWorkerStatuses()` — já existe
- `db.RuntimeMetrics()` — já existe
- SQS `GetQueueAttributes` — já usado no `RuntimeMetricsHandler`
- `AI_RUNTIME_URL` — já é env var no API

---

## 4. Worker Details

### O que resolve

Transforma o worker de caixa preta em componente inspecionável. Mostra dados que realmente existem no banco.

### Backend

**Migration 000006:**
```sql
-- 000006_add_worker_id_to_tasks.up.sql
ALTER TABLE tasks ADD COLUMN worker_id TEXT;
CREATE INDEX idx_tasks_worker_id ON tasks(worker_id);

-- 000006_add_worker_id_to_tasks.down.sql
DROP INDEX IF EXISTS idx_tasks_worker_id;
ALTER TABLE tasks DROP COLUMN IF EXISTS worker_id;
```

**Impacto no worker** (`services/worker/internal/db/tasks.go`):

SetProcessing muda de:
```go
func (d *DB) SetProcessing(ctx context.Context, id string) error {
    tag, err := d.pool.Exec(ctx,
        `UPDATE tasks SET status = 'processing', processing_started_at = COALESCE(processing_started_at, NOW()), updated_at = NOW()
         WHERE id = $1::uuid AND status = 'pending'`, id)
```

Para:
```go
func (d *DB) SetProcessing(ctx context.Context, id, workerID string) error {
    tag, err := d.pool.Exec(ctx,
        `UPDATE tasks SET status = 'processing', processing_started_at = COALESCE(processing_started_at, NOW()), updated_at = NOW(), worker_id = $2
         WHERE id = $1::uuid AND status = 'pending'`, id, workerID)
```

**Impacto cascata:**
- `processor.go` linha 124: `p.db.SetProcessing(processCtx, msg.TaskID)` → `p.db.SetProcessing(processCtx, msg.TaskID, p.workerID)`
- `Processor` struct precisa de campo `workerID string`
- `New()` recebe `workerID` como parâmetro
- `worker/cmd/worker/main.go` passa `hostname` ou env var `WORKER_ID`
- Testes unitários do worker que chamam `SetProcessing` precisam do novo parâmetro

**Novo handler:** `services/api/internal/handlers/workers.go`

```go
type WorkerDetailHandler struct {
    db *db.DB
}
```

**Rota:** `GET /api/workers/{workerID}` — protegida por X-Internal-Token

**Novas queries DB (API side):**

```go
func (d *DB) GetWorkerHeartbeat(ctx context.Context, workerID string) (*WorkerStatus, error) {
    // SELECT worker_id, last_seen_at, tasks_processed, tasks_failed,
    //        current_task_id, goroutines, uptime_seconds,
    //        CASE WHEN last_seen_at < NOW() - INTERVAL '60 seconds' THEN 'stale' ELSE 'healthy' END
    // FROM worker_heartbeats WHERE worker_id = $1
    // Busca por PK — O(1)
}

func (d *DB) GetRecentTasksForWorker(ctx context.Context, workerID string, limit int) ([]TaskEvent, error) {
    // SELECT id, trace_id, status, created_at, completed_at,
    //        COALESCE(error_message, ''), COALESCE(model, '')
    // FROM tasks WHERE worker_id = $1
    // ORDER BY created_at DESC LIMIT $2
    // Usa index idx_tasks_worker_id — O(log n)
}

func (d *DB) GetHealingEventsForWorker(ctx context.Context, workerID string, limit int) ([]HealingEvent, error) {
    // SELECT ... FROM healing_events WHERE worker_id = $1
    // ORDER BY created_at DESC LIMIT $2
    // Sem index dedicado — aceitável (tabela pequena)
}
```

**Response:**
```json
{
  "worker_id": "worker-hostname",
  "status": "healthy",
  "last_seen_at": "2024-01-15T14:34:57Z",
  "heartbeat_age_seconds": 3,
  "uptime_seconds": 9240,
  "tasks_processed": 142,
  "tasks_failed": 3,
  "current_task_id": "uuid-or-null",
  "goroutines": 12,
  "recent_tasks": [
    { "id": "uuid", "trace_id": "abc", "status": "completed", "created_at": "...", "model": "qwen2.5:3b" }
  ],
  "healing_events": [
    { "id": "evt-1", "event_type": "worker.stale", "severity": "warning", "status": "resolved", "created_at": "...", "resolved_at": "..." }
  ]
}
```

### Frontend

**Lista:** Expandir `auto-healing-summary.tsx` — cada worker na lista vira link para `/workers/[workerId]`.

**Página:** `frontend/app/workers/[workerId]/page.tsx`

```
← Back to Dashboard

worker-hostname                        ● Healthy    heartbeat: 3s ago

┌─ Overview ──────────────────────────────────────────┐
│ Uptime           2h 34m                             │
│ Tasks Processed  142                                │
│ Tasks Failed     3                                  │
│ Goroutines       12                                 │
└─────────────────────────────────────────────────────┘

┌─ Current Task ──────────────────────────────────────┐
│ uuid-current    processing    12s elapsed           │
└─────────────────────────────────────────────────────┘

┌─ Recent Tasks ──────────────────────────────────────┐
│ uuid-122    completed    qwen2.5:3b    2m ago       │
│ uuid-121    completed    qwen2.5:3b    5m ago       │
│ uuid-120    failed       —             8m ago       │
└─────────────────────────────────────────────────────┘

┌─ Healing Events ────────────────────────────────────┐
│ worker.stale    warning    resolved    1h ago       │
└─────────────────────────────────────────────────────┘
```

### Teste CI

```
# Worker já roda no E2E
GET /api/operations/summary → extract workers[0].worker_id
GET /api/workers/{worker_id}
  ✓ response 200
  ✓ worker_id matches
  ✓ status is "healthy" or "stale"
  ✓ tasks_processed >= 0
  ✓ uptime_seconds > 0
  ✓ recent_tasks is array
  ✓ healing_events is array

# Worker não existe
GET /api/workers/nonexistent-worker
  ✓ response 404
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/worker/internal/db/tasks.go` | `SetProcessing` adiciona parâmetro `workerID` | **MÉDIO** — muda assinatura |
| `services/worker/internal/processor/processor.go` | Passa `workerID` para `SetProcessing` | Baixo — adicionar campo no struct |
| `services/worker/cmd/worker/main.go` | Passar hostname como workerID | Baixo |
| `infra/database/migrations/000006_*` | Nova migration | Baixo — ADD COLUMN NULL é instantâneo |
| `services/api/internal/http/router.go` | Adicionar rota | Nenhum |
| `services/api/internal/db/operations.go` | Adicionar queries | Nenhum |
| Testes unitários do worker | Atualizar chamadas de `SetProcessing` | **MÉDIO** — precisa verificar |

**Este é o único item que altera código existente de forma não-trivial (assinatura de SetProcessing).**
