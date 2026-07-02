# Phase 11 — Features (Parte 2: DLQ Explorer + Chaos Dashboard)

Baseline, riscos e infraestrutura compartilhada: [phase-11-baseline-and-risks.md](phase-11-baseline-and-risks.md)
Parte 1 (Alert Center + Replay Task): [phase-11-features-part1.md](phase-11-features-part1.md)

---

## 3. DLQ Explorer

### O que resolve

Quando uma task falha 3 vezes e vai para a DLQ, hoje o operador só sabe disso pelo alerta `dlq.nonempty` do watchdog. Não há como:
- Ver quais mensagens estão na DLQ
- Inspecionar o conteúdo (payload, trace_id, task_id)
- Retentar uma mensagem específica (mover de volta para a queue principal)
- Descartar mensagens resolvidas manualmente

O DLQ Explorer dá visibilidade total sobre a dead-letter queue e permite ações de recovery.

### Backend

**Novo handler:** `services/api/internal/handlers/dlq.go`

```go
type DLQHandler struct {
    db        *db.DB
    sqsClient *sqssdk.Client
    dlqURL    string
    mainURL   string
}
```

**Rota 1:** `GET /api/dlq/messages` — Listar mensagens da DLQ

**Lógica:**
1. `ReceiveMessage` com `VisibilityTimeout=0`, `MaxNumberOfMessages=10`, `WaitTimeSeconds=0`
2. Parse cada message body como sqsMessage `{task_id, trace_id, traceparent, payload}`
3. Para cada task_id, buscar status atual no PostgreSQL (pode já ter sido resolvido manualmente)
4. Retornar lista com metadata SQS + task info

```go
func (h *DLQHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    output, err := h.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
        QueueUrl:              aws.String(h.dlqURL),
        MaxNumberOfMessages:   10,
        VisibilityTimeout:     0,
        WaitTimeSeconds:       0,
        MessageAttributeNames: []string{"All"},
        AttributeNames:        []sqstypes.QueueAttributeName{"All"},
    })
    // Parse messages, enrich with DB info, return
}
```

**Response:**
```json
{
  "messages": [
    {
      "message_id": "sqs-msg-id-1",
      "receipt_handle": "opaque-handle-1",
      "task_id": "uuid-task-1",
      "trace_id": "hex-trace-1",
      "payload": "Classify this order...",
      "receive_count": 4,
      "first_received_at": "2024-01-15T10:00:00Z",
      "sent_at": "2024-01-15T09:55:00Z",
      "task_status": "failed",
      "error_message": "ai runtime timeout"
    },
    {
      "message_id": "sqs-msg-id-2",
      "receipt_handle": "opaque-handle-2",
      "task_id": "uuid-task-2",
      "trace_id": "hex-trace-2",
      "payload": "Generate summary...",
      "receive_count": 3,
      "first_received_at": "2024-01-15T12:00:00Z",
      "sent_at": "2024-01-15T11:50:00Z",
      "task_status": "failed",
      "error_message": "model not loaded"
    }
  ],
  "approximate_count": 5,
  "queue_url": "traceruntime-tasks-dlq"
}
```

**Nota sobre `approximate_count`:** Obtido via `GetQueueAttributes` com `ApproximateNumberOfMessages`. SQS é eventually consistent — o count é aproximado. Frontend mostra como "~5 messages" (com til).

**Nota sobre limite:** `ReceiveMessage` retorna no máximo 10 mensagens por chamada. O Explorer mostra até 10 mensagens por refresh. Para filas com mais mensagens, o operador precisa fazer refresh múltiplas vezes (comportamento idêntico ao AWS Console).

**Enriquecimento com PostgreSQL:**
```go
func (d *DB) GetTaskStatus(ctx context.Context, taskID string) (status string, errorMsg string, err error) {
    // SELECT status, COALESCE(error_message, '')
    // FROM tasks WHERE id = $1::uuid
    // Retorna ("", "", nil) se task não encontrada
}
```

---

**Rota 2:** `POST /api/dlq/messages/{messageID}/retry` — Retry mensagem individual

**Importante sobre SQS:** Não existe operação de "re-leitura" de mensagem por ReceiptHandle. O ReceiptHandle serve apenas para `DeleteMessage` e `ChangeMessageVisibility`. Portanto, o frontend DEVE enviar o body e attributes da mensagem no request (obtidos anteriormente via ListMessages).

**Request body:**
```json
{
  "receipt_handle": "opaque-handle-from-list",
  "message_body": "{\"task_id\":\"uuid\",\"trace_id\":\"hex\",\"traceparent\":\"...\",\"payload\":\"...\"}",
  "message_attributes": {
    "traceparent": "00-abc123-def456-01"
  }
}
```

**Lógica:**
1. Validar que `receipt_handle`, `message_body` não estão vazios
2. Publicar mensagem na queue principal (re-enqueue com body/attributes originais):
   ```go
   h.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
       QueueUrl:    aws.String(h.mainURL),
       MessageBody: aws.String(req.MessageBody),
       MessageAttributes: buildAttributes(req.MessageAttributes),
   })
   ```
3. Deletar da DLQ (só após SendMessage bem-sucedido):
   ```go
   h.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
       QueueUrl:      aws.String(h.dlqURL),
       ReceiptHandle: aws.String(req.ReceiptHandle),
   })
   ```
4. Se DeleteMessage falhar (ReceiptHandleIsInvalid):
   - Mensagem já está na queue principal (SendMessage foi OK)
   - Registrar audit event `dlq.message.retried` com warning "delete failed, message may be duplicated"
   - Retornar 200 com warning (não 500 — a operação principal teve sucesso)
5. Reverter task status no DB para `pending`:
   ```go
   h.db.RevertToPendingForRetry(ctx, taskID)
   ```
6. Publicar SSE event `task.retrying`
7. Registrar em healing_events: `dlq.message.retried`

**Ordem de operações (Send → Delete → DB → SSE):**
- Se Send falhar → retorna 500, nada muda
- Se Send OK + Delete falhar → retorna 200 com warning (mensagem pode duplicar, mas operador está informado)
- Se Send OK + Delete OK + DB falhar → retorna 200 (mensagem já na queue, worker vai processar e atualizar DB)

**Nova query DB:**
```go
func (d *DB) RevertToPendingForRetry(ctx context.Context, taskID string) error {
    // UPDATE tasks SET status = 'pending', error_message = NULL, updated_at = NOW()
    // WHERE id = $1::uuid AND status = 'failed'
    // Ignora se task não existe ou status != failed (idempotente)
}
```

**Response (sucesso):**
```json
{
  "status": "retried",
  "task_id": "uuid-task-1",
  "message": "Message moved to main queue"
}
```

**Response (sucesso com warning — delete falhou):**
```json
{
  "status": "retried",
  "task_id": "uuid-task-1",
  "message": "Message sent to main queue but DLQ delete failed (receipt handle expired). Message may appear in both queues temporarily.",
  "warning": true
}
// HTTP 200 (não 500 — operação principal teve sucesso)
```

**Response (receipt_handle expirado no SendMessage attempt):**
```json
{
  "error": "message no longer available (receipt handle expired)"
}
// HTTP 410 Gone
```

**Nota:** O HTTP 410 só ocorre se o ReceiptHandle for usado em alguma operação que o requeira. Como o retry usa o body/attributes (não o handle) para SendMessage, o 410 só acontece se o DeleteMessage falhar — e nesse caso retornamos 200 com warning.

---

**Rota 3:** `DELETE /api/dlq/messages/{messageID}` — Deletar mensagem

**Request body:**
```json
{
  "receipt_handle": "opaque-handle-from-list",
  "task_id": "uuid-task-1",
  "trace_id": "hex-trace-1",
  "payload_preview": "first 200 chars of the message body"
}
```

O frontend envia os metadados da mensagem (obtidos do ListMessages) para que o backend possa registrar auditoria sem precisar re-ler da SQS.

**Lógica:**
1. Registrar auditoria ANTES de deletar (garante trail mesmo se delete falhar):
   ```go
   details, _ := json.Marshal(map[string]string{
       "task_id": req.TaskID, "trace_id": req.TraceID, "payload_preview": req.PayloadPreview,
   })
   d.db.InsertAuditEvent(ctx, "dlq.message.deleted", "info", "operator", details)
   ```
2. Deletar da SQS DLQ:
   ```go
   h.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
       QueueUrl:      aws.String(h.dlqURL),
       ReceiptHandle: aws.String(req.ReceiptHandle),
   })
   ```
3. Se DeleteMessage falhar (ReceiptHandleIsInvalid) → retornar 410 Gone
4. Publicar SSE event `healing.dlq.message.deleted`

**Nova query DB (no API):**
```go
func (d *DB) InsertAuditEvent(ctx context.Context, eventType, severity, source string, details json.RawMessage) error {
    // INSERT INTO healing_events (id, event_type, severity, source, status, details, created_at)
    // VALUES (gen_random_uuid(), $1, $2, $3, 'resolved', $4, NOW())
    //
    // Status 'resolved' porque é uma ação pontual, não uma condição ativa
}
```

**Response (sucesso):**
```json
{
  "status": "deleted",
  "message_id": "sqs-msg-id-1",
  "audit_event_id": "uuid-audit"
}
```

**Response (receipt handle expirado):**
```json
{
  "error": "receipt handle expired, message may have been redelivered"
}
// HTTP 410 Gone
```

---

**Rota 4:** `POST /api/dlq/purge` — Purge toda a DLQ

**Lógica:**
1. Obter count aproximado via `GetQueueAttributes`
2. Registrar auditoria com count
3. `PurgeQueue`:
   ```go
   _, err := h.sqsClient.PurgeQueue(ctx, &sqs.PurgeQueueInput{
       QueueUrl: aws.String(h.dlqURL),
   })
   ```
4. Se erro `PurgeQueueInProgress` → retornar 429 Too Many Requests com mensagem
5. Publicar SSE event `healing.dlq.purged`

**Response (sucesso):**
```json
{
  "status": "purged",
  "approximate_deleted": 5,
  "audit_event_id": "uuid-audit"
}
```

**Response (cooldown ativo):**
```json
{
  "error": "purge already in progress, wait 60s before retrying"
}
// HTTP 429 Too Many Requests
```

**Nota:** SQS `PurgeQueue` pode levar até 60s para completar e não pode ser chamado novamente durante esse período. A API retorna imediatamente e o count será eventualmente consistente. Frontend desabilita botão por 60s após purge.

---

**Rota 5:** `GET /api/dlq/stats` — Estatísticas da DLQ

**Lógica:** `GetQueueAttributes` com attributeNames `ApproximateNumberOfMessages`, `ApproximateNumberOfMessagesNotVisible`

**Response:**
```json
{
  "approximate_messages": 5,
  "approximate_not_visible": 0,
  "oldest_message_age_seconds": 3600,
  "retention_seconds": 604800,
  "retention_remaining_hours": 162
}
```

### Frontend

**Página:** `frontend/app/dlq/page.tsx`

**Layout:**

```
DLQ Explorer                                         Updated: 5s ago

┌─ Stats ────────────────────────────────────────────────────────────┐
│  Messages: ~5    Not Visible: 0    Retention: 7d (162h remaining) │
│  [Purge All]                                                      │
└────────────────────────────────────────────────────────────────────┘

┌─ Messages ─────────────────────────────────────────────────────────┐
│                                                                    │
│ ┌─ Message sqs-msg-id-1 ────────────────────────────────────────┐ │
│ │ Task: uuid-task-1 (failed)    Trace: hex-trace-1              │ │
│ │ Received: 4 times    First: 4h ago    Error: ai runtime tout  │ │
│ │ Payload: "Classify this order for priority routing and..."    │ │
│ │                                                               │ │
│ │ [Retry]  [Delete]  [View Task →]                              │ │
│ └───────────────────────────────────────────────────────────────┘ │
│                                                                    │
│ ┌─ Message sqs-msg-id-2 ────────────────────────────────────────┐ │
│ │ Task: uuid-task-2 (failed)    Trace: hex-trace-2              │ │
│ │ Received: 3 times    First: 2h ago    Error: model not loaded │ │
│ │ Payload: "Generate summary of the following document..."      │ │
│ │                                                               │ │
│ │ [Retry]  [Delete]  [View Task →]                              │ │
│ └───────────────────────────────────────────────────────────────┘ │
│                                                                    │
│                         [Refresh]                                  │
└────────────────────────────────────────────────────────────────────┘
```

**Componentes:**
- `DLQStats` — Approximate count, visibility, retention info
- `DLQMessageCard` — Card expandível com task_id, payload preview, error, actions
- `DLQActions` — Botões Retry/Delete com confirmação

**Interações:**
- [Retry] → Confirmation toast → POST `/api/dlq/messages/{id}/retry` → Remove card + toast "Retried"
- [Delete] → Confirmation modal ("This action is irreversible") → DELETE → Remove card + toast "Deleted"
- [Purge All] → Danger modal ("Delete ALL messages?") → POST `/api/dlq/purge` → Clear list + toast
- [View Task →] → Navega para `/traces/{trace_id}`
- [Refresh] → Re-fetch `/api/dlq/messages`

**Polling:** Stats a cada 10s. Messages NÃO fazem auto-refresh (evita ReceiveMessage excessivo) — refresh manual via botão.

**Navegação:** Adicionar "DLQ" na sidebar (após Alerts)

### Teste CI

```
# Setup: criar task que vai falhar 3x (mock-ai-runtime retorna erro)
# Esperar task ir para DLQ

# Caso 1: Listar mensagens
GET /api/dlq/messages
  ✓ response 200
  ✓ messages is array
  ✓ approximate_count >= 1

# Caso 2: Stats
GET /api/dlq/stats
  ✓ response 200
  ✓ approximate_messages >= 1

# Caso 3: Retry mensagem
POST /api/dlq/messages/{message_id}/retry {"receipt_handle": "..."}
  ✓ response 200
  ✓ status == "retried"
  ✓ task reverted to pending in DB
  ✓ message no longer in DLQ (next list doesn't contain it)
  ✓ task eventually completes (worker processes retry)

# Caso 4: Delete mensagem
# (push another message to DLQ first)
DELETE /api/dlq/messages/{message_id} {"receipt_handle": "..."}
  ✓ response 200
  ✓ status == "deleted"
  ✓ audit_event_id is UUID
  ✓ healing_events table has dlq.message.deleted entry

# Caso 5: Receipt handle expirado
POST /api/dlq/messages/{id}/retry {"receipt_handle": "expired-handle"}
  ✓ response 410

# Caso 6: Purge
POST /api/dlq/purge
  ✓ response 200
  ✓ status == "purged"
GET /api/dlq/stats
  ✓ approximate_messages == 0 (eventually)

# Caso 7: Empty DLQ
GET /api/dlq/messages (when empty)
  ✓ response 200
  ✓ messages == []
  ✓ approximate_count == 0
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/http/router.go` | Registrar 5 rotas novas | Nenhum — adição |
| `services/api/internal/db/tasks.go` | Adicionar `GetTaskStatus()` e `RevertToPendingForRetry()` | Nenhum — queries novas |
| `services/api/internal/db/operations.go` | Adicionar `InsertAuditEvent()` | Nenhum — query nova |
| `infra/database/migrations/000008_*` | Nova migration (remove event_type CHECK, validação move para aplicação) | Baixo — DROP constraint |
| `docker-compose.yml` | Adicionar `SQS_DLQ_URL` no API | Nenhum — env var nova |
| `frontend/components/sidebar.tsx` | Adicionar link "DLQ" | Nenhum |

**Nenhum arquivo existente tem lógica alterada. Todas as mudanças são adições + 1 migration de constraint.**

### Definition of Done

1. Endpoint `/api/dlq/messages` lista mensagens com metadata e task status
2. Endpoint `/api/dlq/messages/{id}/retry` move mensagem para queue principal e reverte task
3. Endpoint `DELETE /api/dlq/messages/{id}` deleta com auditoria
4. Endpoint `/api/dlq/purge` limpa toda a DLQ com auditoria
5. Endpoint `/api/dlq/stats` retorna contadores
6. Frontend mostra DLQ Explorer com cards, actions e stats
7. Retry de mensagem resulta em task sendo reprocessada pelo worker
8. Delete registra auditoria visível no Alert Center
9. Integration tests passam no CI (incluindo cenário de task → DLQ → retry → success)
10. Sidebar atualizada com navegação para DLQ

---

## 4. Chaos Dashboard

### O que resolve

O chaos runner existe como CLI (`cmd/chaos/main.go`) e produz relatórios JSON em `results/`. Hoje o operador precisa:
- Executar via terminal: `make chaos-run SCENARIO=worker-crash`
- Abrir JSON manualmente para ver resultados
- Nenhuma visibilidade histórica de runs anteriores

O Chaos Dashboard permite visualizar relatórios de chaos, disparar cenários via UI, e acompanhar status.

### Decisão de Arquitetura

**O Dashboard NÃO executa chaos diretamente via Docker API.**

Mecanismo de trigger:
1. API recebe POST de trigger → valida cenário → escreve request file em `results/chaos-requests/{run_id}.json`
2. Um container one-shot (`chaos-runner`, profile `chaos`) lê o request e executa
3. Resultado aparece em `results/chaos-suite-{timestamp}.json`
4. Dashboard poll `/api/chaos/status` até report aparecer

**O operador precisa de um trigger externo** para lançar o container. Opções:
- `make chaos-run SCENARIO=X` (existente)
- Docker Compose `--profile chaos up chaos-runner` com env var `CHAOS_SCENARIO`
- Futuro: GitHub Actions manual dispatch

**Para a UI:** O trigger via API apenas registra a intenção. A execução real depende do operador lançar o chaos-runner. O Dashboard mostra: "Scenario queued. Run `make chaos-run` to execute." — ou, se o chaos-runner está configurado como watcher, executa automaticamente.

**Simplificação para v1:** Dashboard é primariamente READ para relatórios + trigger simplificado.

### Backend

**Novo handler:** `services/api/internal/handlers/chaos.go`

```go
type ChaosHandler struct {
    resultsDir string
    requestDir string
}
```

**Rota 1:** `GET /api/chaos/reports` — Listar relatórios de chaos

**Lógica:**
1. Glob `results/chaos-suite-*.json`
2. Para cada arquivo: ler metadata (RunID, Timestamp, Summary) — apenas header, não o JSON inteiro
3. Ordenar por modification time DESC
4. Limitar a últimos 50 reports (evita slow response se diretório acumular centenas)
5. Retornar lista

```go
func (h *ChaosHandler) ListReports(w http.ResponseWriter, r *http.Request) {
    pattern := filepath.Join(h.resultsDir, "chaos-suite-*.json")
    files, _ := filepath.Glob(pattern)
    // Sort by modification time DESC
    // Limit to 50 most recent
    if len(files) > 50 {
        files = files[:50]
    }
    // Read first ~500 bytes of each for metadata (RunID, Timestamp, Summary)
    // Return list
}
```

**Response:**
```json
{
  "reports": [
    {
      "id": "chaos-suite-20240115-143500",
      "file": "chaos-suite-20240115-143500.json",
      "timestamp": "2024-01-15T14:35:00Z",
      "duration_seconds": 480,
      "summary": {
        "total": 6,
        "passed": 5,
        "warned": 1,
        "failed": 0
      },
      "scenarios": ["worker-crash", "runtime-hang", "ai-failure", "postgres-failure", "queue-flood", "slow-inference"]
    }
  ],
  "total": 3
}
```

---

**Rota 2:** `GET /api/chaos/reports/{id}` — Detalhe de um report

**Lógica:** Ler arquivo JSON completo e retornar como response.

**Validação:** `id` deve conter apenas `[a-z0-9-]` (previne path traversal).

```go
func (h *ChaosHandler) GetReport(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if !isValidReportID(id) {
        jsonError(w, "invalid report id", http.StatusBadRequest)
        return
    }
    // filepath.Clean previne traversal mesmo com regex (defesa em profundidade)
    path := filepath.Clean(filepath.Join(h.resultsDir, id+".json"))
    data, err := os.ReadFile(path)
    if errors.Is(err, os.ErrNotExist) {
        jsonError(w, "report not found", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
}

func isValidReportID(id string) bool {
    return regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(id) && len(id) < 100
}
```

**Response:** O JSON completo do SuiteReport (estrutura definida em `cmd/chaos/internal/chaos/runner.go`):
```json
{
  "version": "1.0",
  "run_id": "chaos-suite-20240115-143500",
  "git_commit": "abc123",
  "environment": "local-docker",
  "timestamp": "2024-01-15T14:35:00Z",
  "duration_seconds": 480,
  "summary": {"total": 6, "passed": 5, "warned": 1, "failed": 0},
  "scenarios": [
    {
      "name": "worker-crash",
      "status": "PASS",
      "duration_seconds": 61,
      "stages": {
        "setup": {"status": "ok", "duration_seconds": 5},
        "inject": {"status": "ok", "duration_seconds": 1},
        "observe": {"status": "ok", "duration_seconds": 45},
        "validate": {"status": "ok", "duration_seconds": 8},
        "cleanup": {"status": "ok", "duration_seconds": 2}
      },
      "metrics": {"detection_time_seconds": 3, "recovery_time_seconds": 58},
      "slo_results": [
        {"name": "detection_under_60s", "passed": true, "actual": 3, "threshold": 60}
      ]
    }
  ]
}
```

---

**Rota 3:** `POST /api/chaos/trigger` — Trigger cenário

**Request body:**
```json
{
  "scenario": "worker-crash",
  "timeout_seconds": 300
}
```

**Cenários válidos:** `worker-crash`, `runtime-hang`, `ai-failure`, `postgres-failure`, `queue-flood`, `slow-inference`, `all`

**Lógica:**
1. Validar cenário (whitelist)
2. Verificar que `requestDir` existe e é acessível:
   ```go
   if _, err := os.Stat(h.requestDir); os.IsNotExist(err) {
       jsonError(w, "chaos request directory unavailable", http.StatusInternalServerError)
       return
   }
   ```
3. Gerar `run_id` = `chaos-{scenario}-{timestamp}`
4. Escrever request file:
   ```go
   requestFile := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
   data := ChaosRequest{
       RunID:    runID,
       Scenario: scenario,
       Timeout:  timeoutSeconds,
       QueuedAt: time.Now().UTC(),
   }
   os.WriteFile(requestFile, json.Marshal(data), 0644)
   ```
5. Retornar 202 Accepted com run_id e instruções

**Response:**
```json
{
  "status": "queued",
  "run_id": "chaos-worker-crash-20240115-150000",
  "scenario": "worker-crash",
  "message": "Scenario queued. Execute 'make chaos-run' or wait for chaos-runner watcher.",
  "poll_url": "/api/chaos/status?run_id=chaos-worker-crash-20240115-150000"
}
```

---

**Rota 4:** `GET /api/chaos/status` — Status de execução

**Query params:** `run_id` (obrigatório)

**Lógica:**
1. Verificar se request file existe em `chaos-requests/` → "queued"
2. Verificar se report file existe em `results/` → "completed" (retorna summary)
3. Nenhum dos dois → "not_found"

```go
func (h *ChaosHandler) Status(w http.ResponseWriter, r *http.Request) {
    runID := r.URL.Query().Get("run_id")
    if !isValidReportID(runID) {
        jsonError(w, "invalid run_id", http.StatusBadRequest)
        return
    }
    
    // Check if report exists (execution completed)
    reportPath := filepath.Clean(filepath.Join(h.resultsDir, runID+".json"))
    if _, err := os.Stat(reportPath); err == nil {
        // Read summary from report
        return // status: "completed", summary: {...}
    }
    
    // Check if request exists (still queued)
    requestPath := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
    if _, err := os.Stat(requestPath); err == nil {
        return // status: "queued", queued_at: "..."
    }
    
    // Neither exists
    jsonError(w, "run not found", http.StatusNotFound)
}
```

**Response (queued):**
```json
{
  "run_id": "chaos-worker-crash-20240115-150000",
  "status": "queued",
  "queued_at": "2024-01-15T15:00:00Z"
}
```

**Response (completed):**
```json
{
  "run_id": "chaos-worker-crash-20240115-150000",
  "status": "completed",
  "summary": {"total": 1, "passed": 1, "warned": 0, "failed": 0},
  "report_url": "/api/chaos/reports/chaos-worker-crash-20240115-150000"
}
```

### Frontend

**Página:** `frontend/app/chaos/page.tsx`

**Layout:**

```
Chaos Dashboard

┌─ Trigger Scenario ─────────────────────────────────────────────────┐
│                                                                    │
│  Scenario: [worker-crash ▼]     Timeout: [300s]     [Run Chaos]   │
│                                                                    │
│  Status: ● Queued (run 'make chaos-run' to execute)               │
│  — or —                                                            │
│  Status: ✓ Completed (1 passed, 0 failed)   [View Report →]       │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘

┌─ Recent Reports ───────────────────────────────────────────────────┐
│                                                                    │
│ ┌─ chaos-suite-20240115-143500 ─────────────────────────────────┐ │
│ │ 15 Jan 2024, 14:35    Duration: 8m    6 scenarios             │ │
│ │ ✓ 5 passed  ▲ 1 warned  ✗ 0 failed                           │ │
│ │ [View Details]                                                │ │
│ └───────────────────────────────────────────────────────────────┘ │
│                                                                    │
│ ┌─ chaos-suite-20240114-091200 ─────────────────────────────────┐ │
│ │ 14 Jan 2024, 09:12    Duration: 12m   6 scenarios             │ │
│ │ ✓ 4 passed  ▲ 0 warned  ✗ 2 failed                           │ │
│ │ [View Details]                                                │ │
│ └───────────────────────────────────────────────────────────────┘ │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

**Página de detalhe:** `frontend/app/chaos/[reportId]/page.tsx`

```
← Back to Chaos Dashboard

chaos-suite-20240115-143500
15 Jan 2024, 14:35    Duration: 8m 0s    Git: abc123

┌─ Summary ──────────────────────────────────────────────────────────┐
│  Total: 6    ✓ Passed: 5    ▲ Warned: 1    ✗ Failed: 0           │
└────────────────────────────────────────────────────────────────────┘

┌─ Scenarios ────────────────────────────────────────────────────────┐
│                                                                    │
│ ✓ worker-crash          61s    Detection: 3s    Recovery: 58s     │
│   ├─ setup      5s  ✓                                            │
│   ├─ inject     1s  ✓                                            │
│   ├─ observe   45s  ✓                                            │
│   ├─ validate   8s  ✓                                            │
│   └─ cleanup    2s  ✓                                            │
│   SLOs: detection_under_60s ✓ (3s < 60s)                         │
│                                                                    │
│ ✓ runtime-hang         340s    Detection: 300s   Recovery: 40s    │
│   ├─ setup      3s  ✓                                            │
│   ├─ inject     1s  ✓                                            │
│   ├─ observe  300s  ✓                                            │
│   ├─ validate  30s  ✓                                            │
│   └─ cleanup    6s  ✓                                            │
│                                                                    │
│ ▲ ai-failure           540s    Detection: ...    Recovery: ...     │
│   ├─ setup      3s  ✓                                            │
│   ├─ inject     1s  ✓                                            │
│   ├─ observe  480s  ▲  (warning: slow recovery)                  │
│   ├─ validate  50s  ✓                                            │
│   └─ cleanup    6s  ✓                                            │
│   SLOs: recovery_under_120s ▲ (540s > 120s)                      │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

**Componentes:**
- `ChaosTrigger` — Select de cenário + timeout + botão Run + status indicator
- `ChaosReportCard` — Card resumo com timestamp, duration, pass/warn/fail
- `ChaosReportDetail` — Página completa com scenario breakdown
- `ScenarioTimeline` — Visual de stages (setup→inject→observe→validate→cleanup)
- `SLOBadge` — Pass/Warn/Fail indicator para cada SLO

**Interações:**
- Select scenario → Choose from dropdown (6 cenários + "all")
- [Run Chaos] → POST `/api/chaos/trigger` → Show "Queued" status
- Poll `/api/chaos/status` a cada 5s enquanto status == "queued"
- Quando "completed" → mostrar summary inline + link para report detail
- [View Details] → navega para `/chaos/{reportId}`

**Navegação:** Adicionar "Chaos" na sidebar (último item, após DLQ)

### Teste CI

```
# Chaos reports são gerados pela suite de chaos separada,
# mas podemos testar os endpoints com fixtures

# Setup: colocar fixture JSON em results/

# Caso 1: Listar reports
GET /api/chaos/reports
  ✓ response 200
  ✓ reports is array
  ✓ if reports exist: each has id, timestamp, summary

# Caso 2: Report detail
GET /api/chaos/reports/{id}
  ✓ response 200
  ✓ has version, run_id, scenarios array

# Caso 3: Report não existe
GET /api/chaos/reports/nonexistent
  ✓ response 404

# Caso 4: Path traversal blocked
GET /api/chaos/reports/../../etc/passwd
  ✓ response 400

# Caso 5: Trigger scenario
POST /api/chaos/trigger {"scenario": "worker-crash", "timeout_seconds": 60}
  ✓ response 202
  ✓ status == "queued"
  ✓ run_id starts with "chaos-worker-crash-"
  ✓ request file exists in chaos-requests/

# Caso 6: Trigger invalid scenario
POST /api/chaos/trigger {"scenario": "drop-database"}
  ✓ response 400

# Caso 7: Status check
GET /api/chaos/status?run_id={run_id_from_trigger}
  ✓ response 200
  ✓ status == "queued"

# Caso 8: Empty reports
# (clean results/) → GET /api/chaos/reports
  ✓ response 200
  ✓ reports == []
  ✓ total == 0
```

### Impacto em código existente

| Arquivo | Mudança | Risco |
|---------|---------|-------|
| `services/api/internal/http/router.go` | Registrar 4 rotas novas | Nenhum — adição |
| `docker-compose.yml` | Adicionar bind-mount `results/chaos-requests` (rw) no API | Baixo — novo volume |
| `frontend/components/sidebar.tsx` | Adicionar link "Chaos" | Nenhum |

**Nenhum arquivo existente tem lógica alterada. Todas as mudanças são adições.**

**Mudança no docker-compose (chaos-runner container):**

```yaml
# Adição ao docker-compose.yml (profile chaos)
chaos-runner:
  build:
    context: .
    dockerfile: cmd/chaos/Dockerfile
  profiles: [chaos]
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock
    - ./results:/app/results
    - ./results/chaos-requests:/app/chaos-requests
  environment:
    - DATABASE_URL=postgres://traceruntime:traceruntime@postgres:5432/traceruntime?sslmode=disable
    - AWS_ENDPOINT_URL=http://localstack:4566
    - AWS_ACCESS_KEY_ID=test
    - AWS_SECRET_ACCESS_KEY=test
    - AWS_DEFAULT_REGION=us-east-1
    - SQS_QUEUE_URL=http://localstack:4566/000000000000/traceruntime-tasks
    - SQS_DLQ_URL=http://localstack:4566/000000000000/traceruntime-tasks-dlq
    - CHAOS_SCENARIO=${CHAOS_SCENARIO:-all}
    - CHAOS_OUTPUT_DIR=/app/results
    - WORKER_CONTAINER=traceruntime-worker-1
    - AI_CONTAINER=traceruntime-ai-runtime-1
  networks: [traceruntime]
  depends_on:
    api:
      condition: service_healthy
    worker:
      condition: service_healthy
```

**Makefile target (novo):**
```makefile
chaos-run:
	docker compose --profile chaos run --rm chaos-runner
```

### Definition of Done

1. Endpoint `/api/chaos/reports` lista relatórios existentes ordenados por data
2. Endpoint `/api/chaos/reports/{id}` retorna JSON completo do report
3. Endpoint `/api/chaos/trigger` cria request file e retorna 202
4. Endpoint `/api/chaos/status` mostra status (queued/completed/not_found)
5. Path traversal bloqueado no report ID validation
6. Frontend lista reports com summary cards
7. Frontend mostra detalhe de report com scenario breakdown e SLO badges
8. Frontend trigger form permite selecionar cenário e disparar
9. Frontend poll mostra status em tempo real
10. Chaos runner container definido em docker-compose (profile chaos)
11. `make chaos-run` funciona como trigger
12. Integration tests passam no CI (com fixture JSON)
13. Sidebar atualizada com navegação para Chaos

---

## Resumo de navegação final (sidebar)

```
Dashboard        (existente)
Tasks            (existente)
Traces           (existente)
Alerts           (novo — Phase 11)
DLQ              (novo — Phase 11)
Chaos            (novo — Phase 11)
```

## Resumo de endpoints Phase 11 (completo)

| # | Método | Path | Feature | Status |
|---|--------|------|---------|--------|
| 1 | GET | /api/alerts | Alert Center | Novo |
| 2 | POST | /api/alerts/{id}/acknowledge | Alert Center | Novo |
| 3 | GET | /api/alerts/stats | Alert Center | Novo |
| 4 | POST | /api/tasks/{id}/replay | Replay Task | Novo |
| 5 | GET | /api/dlq/messages | DLQ Explorer | Novo |
| 6 | POST | /api/dlq/messages/{id}/retry | DLQ Explorer | Novo |
| 7 | DELETE | /api/dlq/messages/{id} | DLQ Explorer | Novo |
| 8 | POST | /api/dlq/purge | DLQ Explorer | Novo |
| 9 | GET | /api/dlq/stats | DLQ Explorer | Novo |
| 10 | GET | /api/chaos/reports | Chaos Dashboard | Novo |
| 11 | GET | /api/chaos/reports/{id} | Chaos Dashboard | Novo |
| 12 | POST | /api/chaos/trigger | Chaos Dashboard | Novo |
| 13 | GET | /api/chaos/status | Chaos Dashboard | Novo |

**Total: 13 endpoints novos, 0 endpoints existentes alterados.**

## Dependências entre features

```
Alert Center ──────── independente (pode ser implementado primeiro)
     │
     │ (DLQ Explorer registra audit events visíveis no Alert Center)
     ▼
DLQ Explorer ──────── depende: Alert Center (para audit events)
     
Replay Task ───────── depende: Migration 000007 (independente das outras features)

Chaos Dashboard ───── independente (lê arquivos de disco)
```

**Ordem recomendada mantida:**
1. Alert Center (base para auditoria das outras features)
2. Replay Task (migration + handler, independente)
3. DLQ Explorer (usa audit events do Alert Center)
4. Chaos Dashboard (read-only + trigger, menor urgência)
