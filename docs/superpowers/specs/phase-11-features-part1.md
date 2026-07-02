# Phase 11 — Features (Parte 1: Alert Center + Replay Task)

Baseline, riscos e infraestrutura compartilhada: [phase-11-baseline-and-risks.md](phase-11-baseline-and-risks.md)
Parte 2 (DLQ Explorer + Chaos Dashboard): [phase-11-features-part2.md](phase-11-features-part2.md)

---

## 1. Alert Center

### O que resolve

Hoje o operador vê um contador de healing events no dashboard com uma lista simplificada de incidentes ativos. Não há como:
- Filtrar por tipo, severidade ou período
- Paginar pelo histórico
- Marcar um alerta como "acknowledged" (reconhecido mas não resolvido)
- Ver estatísticas agregadas (frequência, MTTR, distribuição por tipo)

O Alert Center substitui o healing event counter por uma central de alertas completa.

### Backend

**Nova coluna em healing_events:**

**Migration 000006** necessária — a tabela `healing_events` tem CHECK constraint `chk_status` que só aceita `('active', 'resolved')`. A migration expande para incluir `acknowledged`:

```sql
-- 000006_allow_acknowledged_status.up.sql
ALTER TABLE healing_events DROP CONSTRAINT chk_status;
ALTER TABLE healing_events ADD CONSTRAINT chk_status CHECK (status IN ('active', 'acknowledged', 'resolved'));
```

Estados: `active` → `acknowledged` → `resolved`

O watchdog já resolve automaticamente via `reconcileActiveState()`. O acknowledge é ação manual do operador.

**Nota:** O watchdog usa `WHERE status = 'active'` para reconciliation. Um evento `acknowledged` NÃO será auto-resolvido pelo watchdog — permanece acknowledged até a condição desaparecer, quando o watchdog o resolve.

**Impacto no watchdog:** A query de resolve do watchdog é:
```sql
UPDATE healing_events SET status = 'resolved', resolved_at = NOW()
WHERE id = $1::uuid AND status = 'active'
```

Precisa mudar para aceitar também `acknowledged`:
```sql
UPDATE healing_events SET status = 'resolved', resolved_at = NOW()
WHERE id = $1::uuid AND status IN ('active', 'acknowledged')
```

**Este é o ÚNICO ponto que altera código existente no watchdog.**

---

**Novo handler:** `services/api/internal/handlers/alerts.go`

```go
type AlertsHandler struct {
    db *db.DB
}
```

**Rota 1:** `GET /api/alerts` — Listar alertas com filtros

**Query parameters:**
- `status` — `active`, `acknowledged`, `resolved`, `all` (default: `all`)
- `severity` — `info`, `warning`, `critical` (default: todos)
- `event_type` — `worker.stale`, `worker.down`, `queue.lag`, `task.stuck`, `dlq.nonempty` (default: todos)
- `limit` — 1-100 (default: 50)
- `cursor` — ISO timestamp para pagination cursor-based

**Nova query DB:**
```go
func (d *DB) ListAlerts(ctx context.Context, params AlertListParams) ([]HealingEvent, string, error) {
    // SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''),
    //        details, created_at, resolved_at
    // FROM healing_events
    // WHERE 1=1
    //   AND ($1 = 'all' OR status = $1)
    //   AND ($2 = '' OR severity = $2)
    //   AND ($3 = '' OR event_type = $3)
    //   AND ($4 = '' OR created_at < $4::timestamptz)
    // ORDER BY created_at DESC
    // LIMIT $5
    //
    // Retorna events + next_cursor (created_at do último item)
    // Usa idx_healing_events_status_created para filtro por status
    // Usa idx_healing_events_type_created para filtro por type
}
```

**Response:**
```json
{
  "alerts": [
    {
      "id": "uuid",
      "event_type": "worker.stale",
      "severity": "warning",
      "source": "watchdog",
      "status": "active",
      "worker_id": "traceruntime-worker-abc",
      "details": {"threshold_seconds": 60, "actual_seconds": 95},
      "created_at": "2024-01-15T14:30:00Z",
      "resolved_at": null,
      "duration_seconds": 300
    }
  ],
  "next_cursor": "2024-01-15T14:25:00Z",
  "total_active": 3,
  "total_acknowledged": 1
}
```

**Rota 2:** `POST /api/alerts/{id}/acknowledge` — Acknowledge alerta

**Lógica:**
1. Validar `id` como UUID
2. `UPDATE healing_events SET status = 'acknowledged' WHERE id = $1::uuid AND status = 'active'`
3. Se 0 rows affected → 404 (não existe) ou 409 (já acknowledged/resolved)
4. Publicar SSE event `healing.acknowledged` com healing_event_id

**Nova query DB:**
```go
func (d *DB) AcknowledgeAlert(ctx context.Context, id string) error {
    // UPDATE healing_events SET status = 'acknowledged'
    // WHERE id = $1::uuid AND status = 'active'
    //
    // Retorna ErrNotFound se 0 rows affected
}
```

**Response (sucesso):**
```json
{
  "id": "uuid",
  "status": "acknowledged",
  "acknowledged_at": "2024-01-15T14:35:00Z"
}
```

**Rota 3:** `GET /api/alerts/stats` — Estatísticas agregadas

**Nova query DB:**
```go
func (d *DB) AlertStats(ctx context.Context) (*AlertStatsResponse, error) {
    // SELECT
    //   COUNT(*) FILTER (WHERE status = 'active') as active_count,
    //   COUNT(*) FILTER (WHERE status = 'acknowledged') as acknowledged_count,
    //   COUNT(*) FILTER (WHERE status = 'resolved' AND resolved_at > NOW() - INTERVAL '24 hours') as resolved_24h,
    //   COUNT(*) FILTER (WHERE severity = 'critical' AND status = 'active') as critical_active,
    //   AVG(EXTRACT(EPOCH FROM (resolved_at - created_at))) FILTER (WHERE status = 'resolved' AND resolved_at IS NOT NULL) as avg_resolution_seconds
    // FROM healing_events
}
```

**Response:**
```json
{
  "active": 3,
  "acknowledged": 1,
  "resolved_24h": 12,
  "critical_active": 1,
  "avg_resolution_seconds": 145.5,
  "by_type": {
    "worker.stale": {"active": 1, "total_24h": 5},
    "task.stuck": {"active": 2, "total_24h": 7},
    "dlq.nonempty": {"active": 0, "total_24h": 0}
  }
}
```

**Query para `by_type`:**
```go
// SELECT event_type,
//   COUNT(*) FILTER (WHERE status = 'active') as active,
//   COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours') as total_24h
// FROM healing_events
// GROUP BY event_type
```

### Frontend

**Página:** `frontend/app/alerts/page.tsx`

**Layout:**

```
Alert Center                                          Updated: 3s ago

┌─ Stats ────────────────────────────────────────────────────────────┐
│  ● 3 Active    ◐ 1 Acknowledged    ✓ 12 Resolved (24h)           │
│  Avg Resolution: 2m 25s    Critical: 1                            │
└────────────────────────────────────────────────────────────────────┘

┌─ Filters ──────────────────────────────────────────────────────────┐
│  Status: [All ▼]   Severity: [All ▼]   Type: [All ▼]             │
└────────────────────────────────────────────────────────────────────┘

┌─ Alerts ───────────────────────────────────────────────────────────┐
│ ● CRITICAL  dlq.nonempty       —           active    2m ago  [Ack]│
│ ▲ WARNING   worker.stale       worker-abc  active    5m ago  [Ack]│
│ ▲ WARNING   task.stuck         task-123    ack'd     8m ago       │
│ ✓ WARNING   queue.lag          —           resolved  15m ago      │
│ ✓ WARNING   worker.stale       worker-abc  resolved  1h ago       │
│ ...                                                               │
│                        [Load More]                                 │
└────────────────────────────────────────────────────────────────────┘
```

**Componentes:**
- `AlertStatsBar` — Cards com contadores (active, acknowledged, resolved_24h, MTTR)
- `AlertFilters` — Dropdowns para status, severity, event_type
- `AlertList` — Lista paginada com cursor
- `AlertRow` — Row individual com badge de severity, tipo, worker, tempo, botão Ack

**Interações:**
- Clicar [Ack] → POST `/api/alerts/{id}/acknowledge` → atualiza row inline
- Scroll até final → fetch next page com cursor
- Filtros alteram query params → refetch
- SSE updates: novos healing events aparecem no topo em tempo real (já vêm via SSE provider)

**Navegação:** Adicionar "Alerts" na sidebar (entre Dashboard e Tasks)

**Polling:** Stats atualizam a cada 10s. Lista atualiza via SSE (novos eventos) + refetch ao mudar filtro.

### Teste CI

```
# Inserir healing events via watchdog detector (mock) ou diretamente no DB
# Setup: inserir 5 healing events com status/severity variados

# Caso 1: Listar todos
GET /api/alerts?status=all&limit=10
  ✓ response 200
  ✓ alerts array length >= 5
  ✓ each alert has id, event_type, severity, status, created_at
  ✓ ordered by created_at DESC

# Caso 2: Filtrar por status
GET /api/alerts?status=active
  ✓ response 200
  ✓ all alerts have status == "active"

# Caso 3: Filtrar por severity
GET /api/alerts?severity=critical
  ✓ response 200
  ✓ all alerts have severity == "critical"

# Caso 4: Pagination cursor
GET /api/alerts?limit=2
  ✓ response 200
  ✓ alerts length == 2
  ✓ next_cursor is ISO timestamp
GET /api/alerts?limit=2&cursor={next_cursor}
  ✓ response 200
  ✓ alerts[0].created_at < cursor

# Caso 5: Acknowledge
POST /api/alerts/{active_alert_id}/acknowledge
  ✓ response 200
  ✓ response.status == "acknowledged"
GET /api/alerts/{id}... verify status changed

# Caso 6: Acknowledge de alert já resolved
POST /api/alerts/{resolved_alert_id}/acknowledge
  ✓ response 409

# Caso 7: Alert stats
GET /api/alerts/stats
  ✓ response 200
  ✓ active >= 0
  ✓ by_type is object with known event types
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/watchdog/internal/db/healing.go` | Query de resolve aceita `IN ('active', 'acknowledged')` | **Baixo** — 1 linha |
| `services/api/internal/http/router.go` | Registrar 3 rotas novas | Nenhum — adição |
| `infra/database/migrations/000006_*` | Nova migration (expand CHECK constraint) | Baixo — DROP+ADD constraint |
| `docker-compose.yml` | Nenhuma mudança necessária | Nenhum |
| `frontend/components/sidebar.tsx` | Adicionar link "Alerts" | Nenhum |

**Impacto real:** 1 linha alterada no watchdog (WHERE clause) + 1 migration. Todo o resto é adição pura.

### Definition of Done

1. Endpoint `/api/alerts` retorna lista paginada com filtros funcionando
2. Endpoint `/api/alerts/{id}/acknowledge` muda status corretamente
3. Endpoint `/api/alerts/stats` retorna contadores e MTTR
4. Watchdog resolve eventos `acknowledged` normalmente
5. Frontend mostra Alert Center com filtros, pagination e acknowledge
6. SSE events de healing aparecem em tempo real na lista
7. Integration tests passam no CI
8. Sidebar atualizada com navegação para Alerts

---

## 2. Replay Task

### O que resolve

Quando uma task falha (AI timeout, modelo fora, erro transitório), o operador precisa re-executá-la. Hoje a única opção é criar manualmente uma nova task com o mesmo input — se o operador lembrar qual era.

O Replay Task permite re-executar qualquer task `completed` ou `failed` com um clique, preservando rastreabilidade via `replay_of`.

### Backend

**Migration 000007:** `infra/database/migrations/`

```sql
-- 000007_add_replay_fields.up.sql
ALTER TABLE tasks ADD COLUMN replay_of UUID;
ALTER TABLE tasks ADD COLUMN input_payload TEXT;
ALTER TABLE tasks ADD COLUMN input_artifact_key TEXT;
CREATE INDEX idx_tasks_replay_of ON tasks(replay_of);

-- 000007_add_replay_fields.down.sql
DROP INDEX IF EXISTS idx_tasks_replay_of;
ALTER TABLE tasks DROP COLUMN IF EXISTS input_artifact_key;
ALTER TABLE tasks DROP COLUMN IF EXISTS input_payload;
ALTER TABLE tasks DROP COLUMN IF EXISTS replay_of;
```

**Campos de input:**
- `input_payload TEXT` — input textual (usado agora, prompts de texto)
- `input_artifact_key TEXT` — ponteiro para S3 quando input for binário (imagens, áudio, vídeo). NULL por enquanto. Quando inferências multimodais forem suportadas, o API escreve o arquivo em S3 e grava a key aqui.

**Nota:** O campo `artifact_key` existente já serve como `output_artifact_key` — não precisa de outro campo para outputs.

**Lógica de resolução de payload no replay:**
```go
func resolvePayload(task *TaskReplayInfo, s3Client *s3sdk.Client) (string, error) {
    // 1. Se input_artifact_key != "" → fetch do S3 (futuro, inputs binários)
    // 2. Senão, se input_payload != "" → usar direto (atual, texto)
    // 3. Senão → erro 422 (task antiga sem dados de input)
}
```

Hoje apenas o path 2 será executado. O path 1 fica preparado para quando o S3 client existir no API (Request Inspector, Phase 10 Feature 2).

O index em `replay_of` permite queries eficientes para exibir cadeia de replays (Original → Replay 1 → Replay 2).

**Novo handler:** `services/api/internal/handlers/replay.go`

```go
type ReplayHandler struct {
    db        *db.DB
    publisher Publisher
    sqsClient *sqssdk.Client
    sqsURL    string
}
```

**Rota:** `POST /api/tasks/{taskID}/replay` — protegida por X-Internal-Token

**Request body (opcional):**
```json
{
  "override_payload": "optional new input text"
}
```

Se `override_payload` está presente, usa esse texto. Senão, lê `input_payload` do PostgreSQL.

**Lógica:**
1. Validar taskID como UUID
2. `db.GetTaskForReplay(ctx, taskID)` — busca task original
3. Se task não existe → 404
4. Se task.status NOT IN ('completed', 'failed') → 409 Conflict
5. Determinar payload:
   - Se `override_payload` presente no body → usar
   - Senão → usar `task.InputPayload` do PostgreSQL
   - Se `input_payload` é NULL (task criada antes da migration) → 422 Unprocessable Entity
6. Verificar queue depth (admission control) — se cheia → 503
7. Gerar novo taskID + novo traceID
8. Inserir nova task via `db.InsertTask(ctx, params)`
9. Publicar para SQS
10. Publicar SSE event
11. Retornar 201

**Sem S3 — o replay lê exclusivamente do PostgreSQL.** O campo `input_payload` é persistido no momento da criação da task e fica disponível para replay a qualquer momento, sem depender de S3.

**Impacto no Task Handler existente:**

O `TaskHandler.ServeHTTP()` precisa salvar o `input_payload` no INSERT. Para evitar crescimento indefinido de parâmetros posicionais, usar struct:

Atual (`services/api/internal/db/tasks.go`):
```go
func (d *DB) InsertTask(ctx context.Context, id, traceID string) error {
    _, err := d.pool.Exec(ctx,
        `INSERT INTO tasks (id, trace_id, status, created_at, updated_at)
         VALUES ($1::uuid, $2, 'pending', NOW(), NOW())`, id, traceID)
    return err
}
```

Novo:
```go
type CreateTaskParams struct {
    ID               string
    TraceID          string
    InputPayload     string  // texto do prompt (usado agora)
    InputArtifactKey *string // S3 key para inputs binários (futuro, nil por enquanto)
    ReplayOf         *string // nil para tasks normais, UUID para replays
}

func (d *DB) InsertTask(ctx context.Context, params CreateTaskParams) error {
    _, err := d.pool.Exec(ctx,
        `INSERT INTO tasks (id, trace_id, status, created_at, updated_at, input_payload, input_artifact_key, replay_of)
         VALUES ($1::uuid, $2, 'pending', NOW(), NOW(), $3, $4, $5)`,
        params.ID, params.TraceID, params.InputPayload, params.InputArtifactKey, params.ReplayOf)
    return err
}
```

**Impacto cascata:**
- `services/api/internal/http/task.go` → constrói `CreateTaskParams{ID: id, TraceID: traceID, InputPayload: payload}`
- Testes que chamam `InsertTask` precisam usar struct (2-3 call sites)
- Tasks anteriores terão `input_payload = NULL` — replay delas sem `override_payload` retorna 422
- Worker NÃO chama InsertTask (só SetProcessing/SetCompleted) — zero impacto

**Nova query DB para replay:**
```go
func (d *DB) GetTaskForReplay(ctx context.Context, taskID string) (*TaskReplayInfo, error) {
    // SELECT id, trace_id, status, input_payload, model
    // FROM tasks WHERE id = $1::uuid
    //
    // Retorna nil se não existe
}

type TaskReplayInfo struct {
    ID           string
    TraceID      string
    Status       string
    InputPayload *string // NULL para tasks antigas (pre-migration)
    Model        string
}
```

**Flow completo do Replay:**
```
1. POST /api/tasks/{taskID}/replay
2. Validar: task existe, status terminal, payload disponível
3. Gerar novo taskID (UUID) e novo traceID (from span context)
4. InsertTask(CreateTaskParams{ID: new, TraceID: new, InputPayload: payload, ReplayOf: &originalID})
5. Publicar para SQS (mesmo formato do TaskHandler):
   {task_id: newID, trace_id: newTraceID, traceparent: new, payload: payload}
6. Publicar SSE event task.created para o novo task
7. Retornar 201 Created com novo task info
```

**Response (sucesso):**
```json
{
  "task": {
    "id": "new-uuid",
    "trace_id": "new-trace-hex",
    "status": "pending",
    "replay_of": "original-task-uuid",
    "input_payload": "the input text",
    "created_at": "2024-01-15T15:00:00Z",
    "replayed_at": "2024-01-15T15:00:00Z"
  }
}
```

**Response (task em progresso):**
```json
{
  "error": "task still in progress (status: processing)"
}
// HTTP 409 Conflict
```

**Response (payload não disponível):**
```json
{
  "error": "original payload not available (task created before replay support)"
}
// HTTP 422 Unprocessable Entity
```

### Frontend

**Implementação:** Botão "Replay" na tabela de Tasks e na página de Trace Details.

**Tabela Tasks (`frontend/app/tasks/page.tsx`):**
```
┌─ Tasks ────────────────────────────────────────────────────────────┐
│ ID          Status      Model         Duration   Actions          │
│ uuid-123    completed   qwen2.5:3b    31.2s      [Replay] [→]    │
│ uuid-122    failed      qwen2.5:3b    —          [Replay] [→]    │
│ uuid-121    processing  qwen2.5:3b    12s...     [→]             │
│ uuid-120    pending     —             —          [→]             │
└────────────────────────────────────────────────────────────────────┘
```

- Botão [Replay] visível apenas para status `completed` ou `failed`
- Botão [→] navega para trace details

**Modal de Replay:**
```
┌─ Replay Task ─────────────────────────────────────────┐
│                                                       │
│  Original: uuid-123 (completed, 31.2s)                │
│  Model: qwen2.5:3b                                    │
│  Original Input: "Classify this order..."             │
│                                                       │
│  ┌─ Payload ────────────────────────────────────────┐ │
│  │ Classify this order...                           │ │
│  │ (editable textarea, pre-filled with original)    │ │
│  └──────────────────────────────────────────────────┘ │
│                                                       │
│  [ ] Use override payload                             │
│                                                       │
│  [Cancel]                        [Replay Task]        │
└───────────────────────────────────────────────────────┘
```

**Fluxo UI:**
1. Click [Replay] → abre modal com payload original (read-only por default)
2. Check "Use override payload" → textarea editável
3. Click [Replay Task] → POST `/api/tasks/{id}/replay` com body opcional
4. Sucesso → toast "Task replayed" + navega para nova task ou mostra no feed
5. Erro 409 → toast "Task still in progress"
6. Erro 422 → toast "Original payload not available"

**Badge de replay:** Tasks que são replay mostram badge "(replay of uuid-123)" com link para original.

**Replay chain:** Se uma task tem replays, mostrar cadeia: Original → Replay 1 → Replay 2 (query via `idx_tasks_replay_of`).

**Trace Details page:** Adicionar botão [Replay] no header se task é completed/failed.

### Teste CI

```
# Setup: criar task, esperar completar

# Caso 1: Replay de task completed
POST /tasks → wait completed → POST /api/tasks/{task_id}/replay
  ✓ response 201
  ✓ response.task.id != original task_id (novo UUID)
  ✓ response.task.trace_id != original trace_id (novo trace)
  ✓ response.task.replay_of == original task_id
  ✓ response.task.status == "pending"

# Caso 2: Nova task aparece no fluxo
GET /api/events/recent → find new task
  ✓ new task exists with replay_of field

# Caso 3: Replay com override payload
POST /api/tasks/{task_id}/replay {"override_payload": "new input"}
  ✓ response 201
  ✓ response.task.input_payload == "new input"

# Caso 4: Replay de task em progresso
POST /tasks → (don't wait) → POST /api/tasks/{task_id}/replay
  ✓ response 409

# Caso 5: Replay de task inexistente
POST /api/tasks/00000000-0000-0000-0000-000000000000/replay
  ✓ response 404

# Caso 6: Task antiga sem payload
# (insert task diretamente no DB sem input_payload)
POST /api/tasks/{old_task_id}/replay
  ✓ response 422

# Caso 7: Replay com fila cheia
# (flood queue to max depth) → POST /api/tasks/{task_id}/replay
  ✓ response 503

# Caso 8: Replay de replay (cadeia)
POST /tasks → wait completed → replay → wait completed → replay
  ✓ replay2.replay_of == replay1.id (aponta para replay imediato, não para original)
  ✓ cadeia: original ← replay1 ← replay2
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/db/tasks.go` | `InsertTask` passa a receber `CreateTaskParams` struct | **MÉDIO** — muda assinatura |
| `services/api/internal/http/task.go` | Constrói `CreateTaskParams` em vez de args posicionais | Baixo — refactor local |
| `services/api/internal/http/router.go` | Registrar rota POST replay | Nenhum — adição |
| `infra/database/migrations/000007_*` | Nova migration (2 colunas + index) | Baixo — ADD COLUMN NULL |
| `tests/integration/helpers.go` | Atualizar `createTask` se usa InsertTask | Baixo |

**Alteração principal:** `InsertTask` muda de parâmetros posicionais para struct. Impacto cascata controlado:
- `task.go` (handler) — 1 call site
- Integration tests que inserem tasks diretamente (se existirem)
- Worker NÃO chama InsertTask (só SetProcessing/SetCompleted)

### Definition of Done

1. Migration 000007 aplica sem erro e é reversível
2. `InsertTask` persiste `input_payload` em todas as novas tasks
3. Endpoint `/api/tasks/{id}/replay` cria nova task corretamente
4. Nova task aparece na fila SQS e é processada pelo worker normalmente
5. Campo `replay_of` aparece no response de `/api/events/recent`
6. Frontend mostra botão Replay em tasks completed/failed
7. Modal de replay permite override de payload
8. Badge "replay of" aparece em tasks replicadas
9. Replay chain funciona (replay de replay aponta para o replay imediato)
10. Validações de status (409) e payload ausente (422) funcionam
11. Integration tests passam no CI
