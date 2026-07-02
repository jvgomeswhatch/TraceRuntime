# Phase 11 — Baseline, Riscos e Infraestrutura

## Objetivo

Transformar o dashboard de ferramenta de investigação (Phase 10) em plataforma de controle operacional.
O operador deve poder agir sobre o sistema — não apenas observar — diretamente pela interface web.

Cada feature deve ser testável na CI (integration ou E2E).

Este documento descreve o estado atual do sistema, riscos identificados, decisões de arquitetura fixadas, e infraestrutura compartilhada.
As features individuais estão em:
- [Parte 1: Alert Center + Replay Task](phase-11-features-part1.md)
- [Parte 2: DLQ Explorer + Chaos Dashboard](phase-11-features-part2.md)

---

## Decisões de Arquitetura (fixadas)

| # | Decisão | Justificativa |
|---|---------|---------------|
| 1 | Replay Task cria novo task_id + novo trace_id, com campo `replay_of` referenciando a task original | Garante idempotência, rastreabilidade e evita colisão de estado com execuções anteriores |
| 2 | Chaos Dashboard NÃO usa Docker API nem adiciona privilégios a containers | Reutiliza o chaos runner existente (`cmd/chaos/`) executado como processo host; dashboard apenas visualiza relatórios e dispara via endpoint interno |
| 3 | Alert Center é operacional da plataforma apenas — sem notificações externas (email, Slack, webhook) | Scope controlado; notificações externas podem ser adicionadas em fase futura se necessário |

---

## Estado atual do sistema (baseline antes das mudanças)

### API Service (`services/api/`)

- **Router:** chi-based, 10 rotas registradas (Phase 10 incluiu `/api/traces/{traceID}`)
- **Rate limit:** 10 RPS / burst 20, token bucket per-IP
- **Auth:** `X-Internal-Token` middleware em endpoints `/api/*`
- **DB pool:** pgx/v5, `pool_max_conns=5`
- **S3:** NÃO existe client S3 no API (apenas Worker tem S3 write)
- **SQS:** Client existe para queue depth metrics (`GetQueueAttributes`) e task publish (`SendMessage`)
- **Tempo:** Client existe desde Phase 10 (`services/api/internal/tempo/`)
- **Results dir:** `/app/results` bind-mounted read-only para servir relatórios de loadtest/chaos

**Rotas existentes:**

| Método | Path | Auth | Handler |
|--------|------|------|---------|
| GET | /health | Não | Health |
| GET | /metrics | Não | MetricsHandler |
| GET | /events | Não | SSEHandler |
| POST | /tasks | Não | TaskHandler |
| POST | /internal/events | Token | EventsHandler |
| GET | /api/events/recent | Token | RecentEventsHandler |
| GET | /api/operations/summary | Token | OperationsSummaryHandler |
| GET | /api/capacity/latest | Token | CapacityHandler |
| GET | /api/metrics/runtime | Token | RuntimeMetricsHandler |
| GET | /api/traces/{traceID} | Token | TracesHandler |

**Assinatura do NewRouter (atual):**
```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB,
    resultsDir string, allowedOrigins []string, internalToken string,
    sqsClient *sqssdk.Client, sqsQueueURL string) http.Handler
```

### Worker Service (`services/worker/`)

- **SQS polling:** Long-poll com `ReceiveMessage`, max 1 mensagem por poll
- **Process flow:** Parse JSON body → SetProcessing → AI Runtime → S3 write → SetCompleted → Delete SQS msg
- **Retry:** `receiveCount >= 3` → SetFailed permanente; `< 3` → RevertToPending + ChangeVisibility(30s)
- **S3 write:** `{traceID}/{taskID}.json` com payload `{task_id, trace_id, output, model, created_at}`
- **Heartbeat:** UpsertHeartbeat a cada 10s com tasks_processed, tasks_failed, current_task_id, goroutines, uptime

**SQS Message Body (formato atual):**
```json
{
  "task_id": "uuid",
  "trace_id": "hex-32-chars",
  "traceparent": "00-{traceID}-{spanID}-01",
  "payload": "user input text"
}
```

**SQS Message Attributes:**
- `traceparent`: W3C trace context (String type)

### Watchdog Service (`services/watchdog/`)

- **Poll interval:** 15s (configurável via `WATCHDOG_POLL_INTERVAL_SECONDS`)
- **Detection loop:** reconcile → stale workers → worker down → queue lag → stuck tasks → DLQ
- **Event emission:** DB insert + SSE publish + in-memory dedup map
- **Resolution:** Automatic via reconciliation quando condição desaparece
- **Memory:** 64m limit, pool_max_conns=3

**Healing Event Types emitidos:**

| Event Type | Severity | Trigger |
|------------|----------|---------|
| `worker.stale` | warning | Heartbeat > 60s |
| `worker.down` | critical | 3 health check failures consecutivas |
| `queue.lag` | warning | Queue depth > 50 |
| `task.stuck` | warning | Processing > 300s |
| `dlq.nonempty` | critical | DLQ depth > 0 |

**Dedup keys:** `"worker.stale:{workerID}"`, `"worker.down"`, `"queue.lag"`, `"task.stuck:{taskID}"`, `"dlq.nonempty"`

### PostgreSQL Schema (baseline)

**Tabela `tasks`:**
```
id UUID PK
trace_id TEXT NOT NULL
status TEXT (pending|processing|completed|failed)
created_at TIMESTAMPTZ
updated_at TIMESTAMPTZ
processing_started_at TIMESTAMPTZ
completed_at TIMESTAMPTZ
artifact_key TEXT
error_message TEXT
prompt_tokens INT
completion_tokens INT
tokens_per_second FLOAT8
model TEXT
```
Indexes: `idx_tasks_status`, `idx_tasks_trace_id`, `idx_tasks_created_at_desc`

**Tabela `worker_heartbeats`:**
```
worker_id TEXT PK (VARCHAR 255)
last_seen_at TIMESTAMPTZ
tasks_processed BIGINT
tasks_failed BIGINT
current_task_id UUID
goroutines INT
uptime_seconds BIGINT
```

**Tabela `healing_events`:**
```
id UUID PK
event_type TEXT (constrained)
severity TEXT (info|warning|critical)
source TEXT
status TEXT (active|resolved)
worker_id TEXT
details JSONB
created_at TIMESTAMPTZ
resolved_at TIMESTAMPTZ
```
Indexes: `idx_healing_events_type_created`, `idx_healing_events_status_created`

**Pool total atual:** API(5) + Worker(5) + Watchdog(3) = 13 conexões

### SQS Configuration

- **Main queue:** `traceruntime-tasks` — visibility 360s, retention 24h, long-poll 20s, max receive 3
- **DLQ:** `traceruntime-tasks-dlq` — retention 7 dias
- **Endpoint:** `http://localstack:4566/000000000000/traceruntime-tasks`
- **DLQ endpoint:** `http://localstack:4566/000000000000/traceruntime-tasks-dlq`
- **Redrive policy:** maxReceiveCount=3 → DLQ

### S3 Configuration

- **Bucket:** `traceruntime-outputs`
- **Key format:** `{trace_id}/{task_id}.json`
- **Access:** Worker writes via aws-sdk-go-v2, path-style (LocalStack)
- **API:** NÃO lê S3 atualmente (endpoint de inspect da Phase 10 Feature 2 ainda não implementado)

### Chaos Runner (`cmd/chaos/`)

- **Cenários implementados:** 6 (worker-crash, runtime-hang, ai-failure, postgres-failure, queue-flood, slow-inference)
- **Interface Scenario:** Setup → Inject → Observe → Validate → Cleanup
- **Reports:** JSON em `results/chaos-suite-{timestamp}.json`
- **Execução:** CLI `go run cmd/chaos/main.go --scenario=worker-crash`
- **Docker controller:** Kill, stop, start, pause, unpause containers via Docker SDK
- **Checker:** PostgreSQL healing events, heartbeats, SQS queue depth, HTTP health

### Frontend

- **SSE Provider:** EventSource com backoff exponencial (1s→30s), dedup por event_id
- **State:** Max 100 task events + 50 healing events in-memory
- **Healing display:** `operations-summary.tsx` mostra active incidents com count + severity
- **Event feed:** Tabs (all/tasks/operations/errors), mostra merged events
- **Padrão de fetch:** Token via `NEXT_PUBLIC_INTERNAL_TOKEN`, polling 10s para métricas
- **Navigação:** Sidebar com Dashboard, Tasks, Traces

### CI/CD (GitHub Actions)

- **Job 1 — Quality Gate:** Build + lint + typecheck (Go, Python, Frontend)
- **Job 2 — Integration:** Terraform + migrations + smoke test (LocalStack + PostgreSQL)
- **Job 3 — E2E:** Docker Buildx builds, health polling, integration tests, artifact uploads
- **Service containers no E2E:** LocalStack + PostgreSQL (sem Tempo, sem Ollama)
- **Mock AI Runtime:** `tests/integration/cmd/mock-ai-runtime/` para simular inferência

---

## Análise de riscos e mitigações

### RISCO 1: DLQ ReceiveMessage consome mensagens (visibility timeout)

**Problema:** `ReceiveMessage` na DLQ torna a mensagem invisível por `VisibilityTimeout` segundos. Se o Explorer apenas inspeciona sem deletar, a mensagem fica "presa" até o timeout expirar — invisível para outros consumers e para o próprio Explorer em requests subsequentes.

**Mitigação:**
- Usar `VisibilityTimeout=0` no `ReceiveMessage` do Explorer — mensagem permanece visível após leitura
- Alternativa: `ReceiveMessage` com `VisibilityTimeout=5` (mínimo prático) + imediato `ChangeMessageVisibility(0)` após leitura
- DLQ retention de 7 dias garante que mensagens não expiram durante inspeção
- Explorer NÃO deleta mensagens automaticamente — só via ação explícita do operador

```go
output, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
    QueueUrl:            aws.String(dlqURL),
    MaxNumberOfMessages: 10,
    VisibilityTimeout:   0,  // Não esconder mensagem
    WaitTimeSeconds:     0,  // Não fazer long-poll (resposta imediata)
    MessageAttributeNames: []string{"All"},
    AttributeNames:     []sqstypes.QueueAttributeNameAll,
})
```

**Teste:** Integration test valida que mensagem permanece disponível após múltiplas leituras.

### RISCO 2: Replay Task pode sobrecarregar fila com replays em massa

**Problema:** Se o operador dispara replay de muitas tasks simultaneamente, pode exceder o `QUEUE_MAX_DEPTH` admission control ou criar backlog excessivo.

**Mitigação:**
- Rate limit no endpoint de replay: máximo 5 replays por request (batch)
- Cada replay verifica queue depth antes de publicar (mesmo check do TaskHandler)
- Se fila cheia → HTTP 503 com mensagem "Queue full, retry later"
- Frontend desabilita botão de replay quando queue depth > threshold
- Novo campo `replay_of` na tabela tasks permite rastrear cadeia de replays

```go
// Validação no handler de replay
depth := getQueueDepth(ctx)
if depth >= maxQueueDepth {
    jsonError(w, "queue full", http.StatusServiceUnavailable)
    return
}
```

**Teste:** Integration test dispara replay com queue cheia e valida 503.

### RISCO 3: Chaos runner execution via API expõe superfície de ataque

**Problema:** Disparar cenários de chaos via endpoint HTTP permite que qualquer client com token execute ações destrutivas (kill worker, flood queue).

**Mitigação:**
- Endpoint protegido por `X-Internal-Token` (mesmo modelo dos outros endpoints operacionais)
- Chaos execution é **fire-and-forget assíncrono** — endpoint retorna 202 Accepted com run_id
- Execução real acontece no host (não dentro de container) — API apenas sinaliza via filesystem
- Mecanismo: API escreve request file em `results/chaos-requests/` → host script (ou operator) executa
- **Alternativa mais segura (recomendada):** Dashboard é READ-ONLY para chaos — mostra apenas relatórios existentes. Execução permanece via CLI (`make chaos-run SCENARIO=worker-crash`). UI visualiza resultados.
- **Decisão final:** Dashboard é read-only + trigger simplificado. O API NÃO executa chaos diretamente. O trigger funciona via um one-shot container com acesso ao Docker socket (mesmo pattern do Terraform container).

**Teste:** Integration test valida que endpoint de trigger retorna 202 e cria request file.

### RISCO 4: Migration para campo `replay_of` na tabela tasks

**Problema:** Replay Task precisa de um campo `replay_of UUID REFERENCES tasks(id)` para rastrear a task original. Isso é uma ALTER TABLE.

**Mitigação:**
- `ALTER TABLE tasks ADD COLUMN replay_of UUID` — NULL por default, operação instantânea no PostgreSQL
- Sem FK constraint (evita lock na tabela tasks) — validação é lógica, não de schema
- Index NÃO necessário inicialmente (queries por replay_of não são frequentes)
- Tasks existentes terão `replay_of = NULL` — compatibilidade retroativa total
- Migration reversível: `ALTER TABLE tasks DROP COLUMN replay_of`

**Impacto:** Zero em queries existentes. Worker ignora o campo (não lê nem escreve `replay_of`).

### RISCO 5: Alert Center queries em healing_events com volume crescente

**Problema:** A tabela `healing_events` não tem TTL nem cleanup. Com o tempo, queries de listagem podem ficar lentas.

**Mitigação:**
- Queries do Alert Center SEMPRE usam `LIMIT` (max 100 por página)
- Indexes existentes cobrem os access patterns:
  - `idx_healing_events_status_created` → filtro por status + ORDER BY created_at DESC
  - `idx_healing_events_type_created` → filtro por event_type + ORDER BY
- Pagination via cursor (`created_at < $last_seen`) — evita OFFSET
- Volume esperado: ~10-50 eventos/dia em operação normal, ~200-500 durante chaos
- Com 7 dias de operação intensa: ~3500 rows — PostgreSQL lida sem problema
- Cleanup futuro (opcional): cron job que arquiva eventos > 30 dias

**Teste:** Integration test insere 100+ eventos e valida que queries com filtro + limit retornam em < 100ms.

### RISCO 6: DLQ Explorer delete é irreversível

**Problema:** Operador pode deletar mensagem da DLQ que precisaria para investigação futura.

**Mitigação:**
- Delete requer confirmação no frontend (modal de confirmação)
- Antes de deletar: API persiste metadados da mensagem em `healing_events` com `event_type = "dlq.message.deleted"` e `details` contendo o body original
- Isso garante auditabilidade — mensagem deletada da SQS ainda tem registro no PostgreSQL
- Alternativa mais simples: NÃO implementar delete individual — apenas retry (que move da DLQ para main queue) e purge (que limpa toda a DLQ)

**Decisão:** Implementar delete individual COM registro de auditoria em healing_events.

### RISCO 7: Chaos container one-shot precisa de Docker socket

**Problema:** Para disparar chaos via Dashboard, o container precisa de acesso ao Docker socket para manipular outros containers.

**Mitigação:**
- Chaos runner container NÃO roda como parte do stack normal — é profile `chaos` separado
- Container usa `docker.sock` bind-mount (mesmo pattern que o Terraform container usa para provisioning)
- Container é one-shot (`service_completed_successfully`) — não persiste
- Sem `--privileged` — apenas bind de `/var/run/docker.sock`
- API cria um trigger file; Makefile/script verifica e lança o container
- Dashboard polling verifica se report apareceu em `results/`

```yaml
chaos-runner:
  build:
    context: .
    dockerfile: cmd/chaos/Dockerfile
  profiles: [chaos]
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock
    - ./results:/app/results
  environment:
    - DATABASE_URL=postgres://traceruntime:traceruntime@postgres:5432/traceruntime?sslmode=disable
    - AWS_ENDPOINT_URL=http://localstack:4566
    - SCENARIO=${CHAOS_SCENARIO:-all}
  networks: [traceruntime]
  depends_on:
    api:
      condition: service_healthy
    worker:
      condition: service_healthy
```

**Teste:** E2E test verifica que `make chaos-run SCENARIO=worker-crash` gera report em `results/`.

### RISCO 8: Replay de task failed vs completed

**Problema:** Replay de task `completed` faz sentido (re-executar com novo modelo/payload). Mas replay de task `pending` ou `processing` pode criar estado inconsistente.

**Mitigação:**
- Replay permitido APENAS para tasks com status `completed` ou `failed`
- Tasks em `pending` ou `processing` → HTTP 409 Conflict ("task still in progress")
- Validação no handler antes de criar nova task
- Frontend desabilita botão de replay para tasks não-terminais

**Teste:** Integration test tenta replay de task `processing` e valida 409.

### RISCO 9: SQS ReceiveMessage na DLQ pode retornar mensagens duplicadas

**Problema:** SQS é eventually consistent. `ReceiveMessage` com `VisibilityTimeout=0` pode retornar a mesma mensagem múltiplas vezes em requests consecutivos.

**Mitigação:**
- Frontend deduplica por `MessageId` (SQS garante unicidade do MessageId)
- Backend retorna `message_id` no response para que frontend possa deduplicar
- Explorer mostra "approximate" count (mesmo approach do AWS Console)
- Não é um problema operacional grave — operador vê a mesma mensagem 2x mas não perde dados

**Teste:** Não testável deterministicamente em CI (comportamento eventual). Documentar como known behavior.

### RISCO 10: NewRouter signature já tem 9 parâmetros

**Problema:** Adicionar mais dependências (DLQ URL, chaos request dir) inflaria ainda mais a assinatura.

**Mitigação:**
- Mesma estratégia da Phase 10: novos valores lidos DENTRO do NewRouter a partir de env vars
- `SQS_DLQ_URL` lido via `os.Getenv()` dentro do router (não como parâmetro)
- `CHAOS_REQUEST_DIR` lido via `os.Getenv()` dentro do router
- Assinatura do NewRouter NÃO muda

```go
// Dentro de NewRouter:
dlqURL := os.Getenv("SQS_DLQ_URL")
chaosRequestDir := os.Getenv("CHAOS_REQUEST_DIR") // default: /app/chaos-requests
```

**Impacto:** Adicionar `SQS_DLQ_URL` como env var no docker-compose para o API. Nenhuma mudança na assinatura.

---

## Infraestrutura compartilhada

### Mudanças no docker-compose.yml

```yaml
api:
  environment:
    # Existentes (sem mudança):
    # DATABASE_URL, QUEUE_BACKEND, SQS_QUEUE_URL, OTEL_EXPORTER_OTLP_ENDPOINT,
    # ALLOWED_ORIGINS, INTERNAL_TOKEN, AI_RUNTIME_URL, AWS_*, TEMPO_URL,
    # CAPACITY_RESULTS_DIR
    
    # Novos (Phase 11):
    SQS_DLQ_URL: http://localstack:4566/000000000000/traceruntime-tasks-dlq
    # S3_BUCKET não necessário para Phase 11 (Replay lê payload do PostgreSQL, não do S3)

  volumes:
    - ./results:/app/results:ro           # Existente (chaos reports + loadtest)
    - ./results/chaos-requests:/app/chaos-requests:rw  # Novo (chaos trigger files)
```

### Migration 000006 (Alert Center — acknowledged status)

```sql
-- 000006_allow_acknowledged_status.up.sql
ALTER TABLE healing_events DROP CONSTRAINT chk_status;
ALTER TABLE healing_events ADD CONSTRAINT chk_status CHECK (status IN ('active', 'acknowledged', 'resolved'));

-- 000006_allow_acknowledged_status.down.sql
UPDATE healing_events SET status = 'active' WHERE status = 'acknowledged';
ALTER TABLE healing_events DROP CONSTRAINT chk_status;
ALTER TABLE healing_events ADD CONSTRAINT chk_status CHECK (status IN ('active', 'resolved'));
```

Operação instantânea — DROP + ADD constraint não reescreve tabela.
Down migration reverte acknowledged → active antes de restaurar constraint.

### Migration 000007 (Replay Task — replay fields + multimodal prep)

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

Operação instantânea — `ADD COLUMN` com NULL default não reescreve tabela.

- `input_payload` — texto do prompt (usado agora)
- `input_artifact_key` — ponteiro S3 para inputs binários/multimodais (futuro, NULL por enquanto)
- `replay_of` — UUID da task original que gerou este replay
- `idx_tasks_replay_of` — permite queries de cadeia de replays

### Migration 000008 (DLQ Explorer — remove event_type CHECK constraint)

A tabela `healing_events` tem CHECK constraint `healing_events_event_type_check` que limita os valores de `event_type`. Cada nova feature que adiciona event types exige migration de DROP+ADD constraint.

Para tabelas operacionais que tendem a ganhar novos tipos ao longo do tempo, a validação na aplicação (whitelist em Go) é mais pragmática que CHECK constraints.

```sql
-- 000008_remove_event_type_check.up.sql
ALTER TABLE healing_events DROP CONSTRAINT IF EXISTS healing_events_event_type_check;

-- 000008_remove_event_type_check.down.sql
ALTER TABLE healing_events ADD CONSTRAINT healing_events_event_type_check CHECK (
    event_type IN ('worker.stale', 'worker.down', 'queue.lag', 'task.stuck', 'dlq.nonempty', 'dlq.growing')
);
```

Validação na aplicação (watchdog e API):
```go
var validEventTypes = map[string]bool{
    "worker.stale": true, "worker.down": true, "queue.lag": true,
    "task.stuck": true, "dlq.nonempty": true, "dlq.growing": true,
    "dlq.message.deleted": true, "dlq.message.retried": true, "dlq.purged": true,
}
```

Novos event types são adicionados via código (1 linha), não via migration.

### Novas rotas (resumo)

| Método | Path | Auth | Feature |
|--------|------|------|---------|
| GET | /api/alerts | Token | Alert Center — listar alertas com filtros |
| POST | /api/alerts/{id}/acknowledge | Token | Alert Center — acknowledge alerta |
| GET | /api/alerts/stats | Token | Alert Center — estatísticas agregadas |
| POST | /api/tasks/{id}/replay | Token | Replay Task — re-executar task |
| GET | /api/dlq/messages | Token | DLQ Explorer — listar mensagens |
| POST | /api/dlq/messages/{id}/retry | Token | DLQ Explorer — retry mensagem |
| DELETE | /api/dlq/messages/{id} | Token | DLQ Explorer — deletar mensagem |
| POST | /api/dlq/purge | Token | DLQ Explorer — purge all |
| GET | /api/dlq/stats | Token | DLQ Explorer — estatísticas |
| GET | /api/chaos/reports | Token | Chaos Dashboard — listar reports |
| GET | /api/chaos/reports/{id} | Token | Chaos Dashboard — report detail |
| POST | /api/chaos/trigger | Token | Chaos Dashboard — trigger scenario |
| GET | /api/chaos/status | Token | Chaos Dashboard — status da execução |

### Checklist de segurança

- [ ] Todos os endpoints novos protegidos por `internalTokenMiddleware`
- [ ] Rate limit existente (10 RPS) cobre automaticamente todos os endpoints novos
- [ ] DLQ ReceiveMessage com `VisibilityTimeout=0` (não esconde mensagens)
- [ ] Replay valida status da task original (só completed/failed)
- [ ] Replay verifica queue depth antes de publish (admission control)
- [ ] DLQ delete registra auditoria em healing_events antes de DeleteMessage
- [ ] Chaos trigger NÃO executa diretamente — cria request file para execução externa
- [ ] Replay lê payload exclusivamente do PostgreSQL (sem dependência de S3)
- [ ] Todas as queries novas com `context.WithTimeout(ctx, 3*time.Second)`
- [ ] MessageId da SQS nunca usado como input para queries SQL (previne injection)

### Ordem de implementação

```
1. Alert Center       — migration 000006 (acknowledged status) + handler + UI
2. Replay Task        — migration 000007 (replay fields) + handler + reutiliza Publisher + UI
3. DLQ Explorer       — migration 000008 (audit event types) + SQS ReceiveMessage/Delete + handler + UI
4. Chaos Dashboard    — lê results/ + chaos trigger file + UI (read-heavy)
```

**Justificativa:**
1. Alert Center primeiro — migration mínima, reutiliza 100% da infra existente
2. Replay Task — migration para campos novos, reutiliza Publisher existente
3. DLQ Explorer — migration para audit types, adiciona operações SQS novas ao API
4. Chaos Dashboard por último — mais complexo (trigger mechanism), menor urgência operacional

### Resumo de impacto

| Categoria | Arquivos alterados | Arquivos novos | Migration | Risco geral |
|-----------|-------------------|----------------|-----------|-------------|
| Alert Center | 2 (watchdog resolve query, router setup) | 4 (handler, db queries, frontend page, components) | 000006 | Baixo |
| Replay Task | 2 (InsertTask signature, task handler) | 5 (handler, db queries, migration, frontend modal, component) | 000007 | Baixo-Médio |
| DLQ Explorer | 1 (router setup) | 6 (handler, sqs ops, db queries, migration, frontend page, components) | 000008 (remove constraint) | Baixo-Médio |
| Chaos Dashboard | 1 (router setup) | 6 (handler, trigger, Dockerfile, Makefile target, frontend page, components) | Não | Médio |

**Total: 6 arquivos existentes alterados, ~20 arquivos novos, 3 migrations (000006, 000007, 000008).**
