# Phase 10 — Baseline, Riscos e Infraestrutura

## Objetivo

Transformar o dashboard de visualização de métricas em ferramenta de investigação operacional.
Cada feature deve ser testável na CI (integration ou E2E).

Este documento descreve o estado atual do sistema, riscos identificados, e infraestrutura compartilhada.
As features individuais estão em [phase-10-features.md](phase-10-features.md).

---

## Estado atual do sistema (baseline antes das mudanças)

### API Service (`services/api/`)

- **Tracer:** `traceruntime-api`, exporta gRPC para `otel-collector:4317`
- **DB pool:** pgx/v5, `pool_max_conns=5`, sem query timeout explícito (pgx default ~30s connection timeout)
- **HTTP server:** `ReadTimeout: 10s`, `WriteTimeout: 0` (SSE precisa disso), `IdleTimeout: 120s`
- **Rate limit:** 10 RPS / burst 20, token bucket per-IP, cleanup 5min
- **Auth:** `X-Internal-Token` middleware, bypass quando env var vazia
- **S3:** NÃO existe client S3 no API. Só o Worker tem S3.
- **Tempo:** NÃO é consultado pelo API hoje. Spans são enviados via OTEL, mas nunca lidos de volta.
- **Error pattern:** `jsonError(w, "message", statusCode)` → `{"error": "message"}`

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

**Assinatura do NewRouter:**
```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB,
    resultsDir string, allowedOrigins []string, internalToken string,
    sqsClient *sqssdk.Client, sqsQueueURL string) http.Handler
```

### Worker Service (`services/worker/`)

- **DB pool:** pgx/v5, `pool_max_conns=5`
- **Spans criados:** `task.process` (root), propaga traceparent para AI runtime via HTTP header
- **S3:** Escreve artifacts em `{trace_id}/{task_id}.json` no bucket `traceruntime-outputs`
- **NÃO grava worker_id na tabela tasks.** Worker ID só existe em `worker_heartbeats`.
- **SetProcessing:** `UPDATE tasks SET status = 'processing' WHERE status = 'pending'` — sem worker_id
- **SetCompleted:** `UPDATE tasks SET status = 'completed', artifact_key = $1, prompt_tokens, completion_tokens, tokens_per_second, model`

### AI Runtime (`ai-runtime/`)

- **Spans criados:** `ai.infer` (root), `graph.classify`, `graph.generate`, `graph.validate`
- **Attributes nos spans:** `task_id`, `execution_status`, `task_type`, `model`, `validation_status`

### Tempo

- **Retention:** 1 hora (`block_retention: 1h`)
- **Query SLO:** 5 segundos para `trace_by_id`
- **API HTTP:** `tempo:3200/api/traces/{traceID}` — formato protobuf por padrão, JSON com header `Accept: application/json`
- **Rede:** API e Tempo estão na mesma rede Docker `traceruntime` — API pode alcançar `tempo:3200`

### PostgreSQL

- **Tabela tasks:** id, trace_id, status, created_at, updated_at, processing_started_at, completed_at, artifact_key, error_message, prompt_tokens, completion_tokens, tokens_per_second, model
- **NÃO tem:** worker_id
- **Indexes:** status, trace_id, created_at DESC
- **Pool total:** API(5) + Worker(5) + Watchdog(3) = 13 conexões

### Frontend

- **SSE Provider:** conecta em `/events`, fetch único de `/api/events/recent` e `/api/operations/summary` no mount
- **Traces page:** agrupa SSE events por trace_id, mostra pipeline de 6 estágios — NÃO consulta Tempo
- **Tasks page:** agrupa SSE events por task_id, mostra status/tokens
- **shadcn/ui instalado:** Badge, Button, Card, Input
- **Padrão de fetch:** token via `process.env.NEXT_PUBLIC_INTERNAL_TOKEN`

### Spans que existem no Tempo hoje

| Service | Operation | Quando | Attributes |
|---------|-----------|--------|------------|
| `traceruntime-api` | `POST /tasks` | Request HTTP chega | http.method, http.path |
| `traceruntime-api` | `task.create` | Dentro do handler | task_id |
| `traceruntime-worker` | `task.process` | Worker pega mensagem do SQS | task_id |
| `traceruntime-ai-runtime` | `ai.infer` | Chamada de inferência | task_id, execution_status |
| `traceruntime-ai-runtime` | `graph.classify` | Classificação do prompt | task_type |
| `traceruntime-ai-runtime` | `graph.generate` | Geração Ollama | model, truncated |
| `traceruntime-ai-runtime` | `graph.validate` | Validação do output | validation_status |

**O que NÃO tem span hoje:**
- Queue wait time (entre SQS publish e worker receive) — derivável dos timestamps no DB
- S3 write — NÃO tem span. Duração estimável: `completed_at - (processing_started_at + inference_duration_ms)`

---

## Análise de riscos e mitigações

### RISCO 1: Tempo query timeout/indisponibilidade

**Problema:** O API nunca consultou o Tempo antes. Se o Tempo estiver lento ou fora, o endpoint de trace details pode travar.

**Mitigação:**
- HTTP client com timeout de 5s (compatível com o SLO do Tempo `trace_by_id: 5s`)
- Context com deadline propagado do request
- Se Tempo retornar erro/timeout → retornar response parcial com dados do PostgreSQL (task info sem spans)
- Nunca travar handler esperando Tempo — falha graceful

```go
tempoClient := &http.Client{Timeout: 5 * time.Second}
```

**Teste:** Integration test deve validar que endpoint retorna 200 mesmo com Tempo indisponível (response parcial).

### RISCO 2: Pool de conexões PostgreSQL (5 conns)

**Problema:** Novos endpoints adicionam queries. Se 4 requests chegam simultaneamente pedindo trace details + task inspect, pode esgotar o pool.

**Análise:** Rate limit já está em 10 RPS com burst 20. Mas os endpoints novos fazem queries mais pesadas (GetTask + GetWorkerDetail). Com pool de 5 e query sem timeout explícito, um query lento pode bloquear o pool inteiro.

**Mitigação:**
- Adicionar context timeout de 3s em TODAS as queries novas
- Não aumentar pool (5 é adequado para 14GB/256m do container API)
- Novas queries devem ser simples (SELECT por PK ou index existente, zero joins pesados)

```go
ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
defer cancel()
```

**Impacto em queries existentes:** Zero. Queries existentes não são alteradas. Novas queries são adicionais.

### RISCO 3: S3 client no API (não existe hoje)

**Problema:** Request Inspector precisa buscar artifact do S3. Hoje o API não tem S3 client.

**Mitigação:**
- Adicionar S3 client ao API via aws-sdk-go-v2 (mesmo pattern do Worker)
- Compartilhar `awsconfig.LoadDefaultConfig` que já existe para SQS
- S3 GetObject com timeout de 3s
- Se S3 falhar → retornar response sem campo `artifact` (graceful degradation)
- NÃO impacta nenhum código existente — é adição pura

**Config:** Reutilizar `AWS_ENDPOINT_URL` e `AWS_REGION` que já existem no docker-compose.

### RISCO 4: Tempo retention de 1 hora

**Problema:** Traces desaparecem após 1h. Se o usuário abrir trace details de uma task antiga, o Tempo retorna 404.

**Mitigação:**
- Frontend mostra dados do PostgreSQL (task metadata, tokens, timestamps) SEMPRE
- Spans do Tempo são enriquecimento opcional — se não existem, mostra mensagem "Trace expired (retention: 1h)"
- NÃO aumentar retention (memória limitada, 512m para Tempo)
- Considerar: calcular duração por estágio a partir dos timestamps do PostgreSQL (processing_started_at, completed_at) como fallback

**Teste:** Validar que endpoint retorna 200 com `spans: null` ou `spans: []` quando trace expirou.

### RISCO 5: Worker não grava worker_id na tabela tasks

**Problema:** Worker Details precisa saber quais tasks um worker processou. Hoje `worker_id` só existe em `worker_heartbeats`.

**Mitigação:**
- Migration 000006: `ALTER TABLE tasks ADD COLUMN worker_id TEXT`
- Index: `CREATE INDEX idx_tasks_worker_id ON tasks(worker_id)`
- Worker `SetProcessing` passa a incluir `worker_id` no UPDATE
- **Retrocompatibilidade:** tasks antigas terão `worker_id = NULL` — queries usam `WHERE worker_id = $1` que naturalmente ignora NULLs
- Worker Details mostra "No historical data" para tasks processadas antes da migration

**Risco da migration:**
- `ALTER TABLE ADD COLUMN` com default NULL é operação instantânea no PostgreSQL (não reescreve tabela)
- `CREATE INDEX CONCURRENTLY` seria ideal mas não é suportado pelo migrate — index simples em tabela pequena (< 10k rows) é instantâneo
- Migration reversível: `000006_add_worker_id_to_tasks.down.sql` com `DROP INDEX` + `ALTER TABLE DROP COLUMN`

**Impacto no worker:**
- `SetProcessing` muda de:
  ```sql
  UPDATE tasks SET status = 'processing', processing_started_at = COALESCE(processing_started_at, NOW()), updated_at = NOW()
  WHERE id = $1::uuid AND status = 'pending'
  ```
  Para:
  ```sql
  UPDATE tasks SET status = 'processing', processing_started_at = COALESCE(processing_started_at, NOW()), updated_at = NOW(), worker_id = $2
  WHERE id = $1::uuid AND status = 'pending'
  ```
- Assinatura muda: `SetProcessing(ctx, id string)` → `SetProcessing(ctx, id, workerID string)`
- **Impacto cascata:** `Processor.Process()` precisa passar `workerID` para `SetProcessing`. O workerID já está disponível via hostname/env var no worker main.go.

### RISCO 6: Topology handler faz múltiplas chamadas HTTP

**Problema:** `/api/runtime/topology` precisa verificar health de cada serviço. Se AI Runtime estiver fora, o handler trava esperando.

**Mitigação:**
- Health checks em paralelo via goroutines com `errgroup`
- Timeout de 2s por health check
- Se serviço não responder → node status = "down" (não travar)
- Total timeout do handler: 5s (context deadline)
- SQS GetQueueAttributes já tem timeout via aws-sdk default

```go
g, gCtx := errgroup.WithContext(ctx)
g.Go(func() error { /* check ai-runtime health */ })
g.Go(func() error { /* check worker health */ })
g.Wait()
```

**Impacto:** Nenhum em código existente. Handler novo, independente.

### RISCO 7: Frontend polling de topology a cada 10s

**Problema:** Polling frequente pode sobrecarregar o API. Cada poll faz health checks de todos os serviços.

**Mitigação:**
- Cache server-side com TTL de 5s — se dois requests chegam em 5s, retorna cache
- Rate limit já protege (10 RPS global)
- Topology handler é leve (health checks em paralelo, 2s timeout cada)
- Frontend usa `setInterval` com cleanup no `useEffect` — sem memory leak

### RISCO 8: Alteração na assinatura de NewRouter

**Problema:** Novos handlers precisam de novos parâmetros (Tempo client, S3 client, URLs de serviços). NewRouter já tem 9 parâmetros.

**Mitigação:**
- Criar Tempo client e S3 client dentro do NewRouter a partir de env vars (sem mudar assinatura)

```go
// Dentro de NewRouter:
tempoURL := os.Getenv("TEMPO_URL") // default: http://tempo:3200
tempoClient := tempo.New(tempoURL)

s3Cfg, _ := awsconfig.LoadDefaultConfig(context.Background())
s3Client := s3.NewFromConfig(s3Cfg, func(o *s3.Options) { o.UsePathStyle = true })
```

**Impacto:** Adicionar `TEMPO_URL` e `S3_BUCKET` como env vars no docker-compose para o API. Nenhuma mudança em código existente.

### RISCO 9: Tempo retorna protobuf, não JSON

**Problema:** A API HTTP do Tempo retorna protobuf por padrão. O Go API precisa parsear.

**Mitigação:**
- Usar header `Accept: application/json` no request para Tempo — Tempo suporta JSON response
- Formato JSON do Tempo: `{"batches": [{"resource": {...}, "scopeSpans": [{"spans": [...]}]}]}` (OTLP JSON)
- Parser Go precisa transformar OTLP JSON → formato interno simplificado
- Se parsing falhar → log error, retornar response sem spans

### RISCO 10: Testes de integração no CI (sem Tempo)

**Problema:** CI usa mock-ai-runtime e não tem Tempo. Trace Details endpoint precisa funcionar sem Tempo.

**Mitigação:**
- Endpoint retorna dados do PostgreSQL mesmo sem Tempo (graceful degradation)
- CI testa: response 200, task metadata presente, spans = null/empty (aceitável)
- Teste local com `make up-full` valida integração real com Tempo

---

## Infraestrutura compartilhada

### Mudanças no docker-compose.yml

```yaml
api:
  environment:
    # Existentes (sem mudança):
    # DATABASE_URL, QUEUE_BACKEND, SQS_QUEUE_URL, OTEL_EXPORTER_OTLP_ENDPOINT,
    # ALLOWED_ORIGINS, INTERNAL_TOKEN, AI_RUNTIME_URL, AWS_*
    
    # Novos:
    TEMPO_URL: http://tempo:3200              # Para Trace Details e Request Inspector
    S3_BUCKET: traceruntime-outputs           # Para Request Inspector (artifact fetch)
    DLQ_URL: ${DLQ_URL}                       # Para Topology (DLQ depth check)
```

### Checklist de segurança

- [ ] Validar traceID como hex string 32 chars antes de enviar para Tempo (previne injection no URL)
- [ ] Validar taskID como UUID antes de query DB (pgx já faz cast `$1::uuid`)
- [ ] Validar workerID como string alfanumérica antes de query DB
- [ ] Todos os endpoints novos protegidos por `internalTokenMiddleware`
- [ ] S3 GetObject com timeout 3s (previne hang)
- [ ] Tempo GetTrace com timeout 5s (previne hang)
- [ ] Health checks do topology com timeout 2s cada
- [ ] Artifact content retornado como JSON (nunca como raw bytes — previne XSS)
- [ ] Rate limit existente (10 RPS) cobre todos os endpoints novos automaticamente

### Ordem de implementação

```
1. Trace Details       — cria tempo.Client, não altera código existente
2. Request Inspector   — reutiliza tempo.Client, adiciona S3 client
3. Runtime Topology    — reutiliza queries existentes, handler isolado
4. Worker Details      — migration + alteração no worker (mais arriscado, por último)
```

**Justificativa:**
1. Trace Details primeiro — zero impacto em código existente, cria o `tempo.Client` reutilizado depois
2. Request Inspector — depende do tempo.Client, adiciona S3 (risco controlado)
3. Runtime Topology — totalmente independente, reutiliza queries existentes
4. Worker Details por último — única feature que altera código existente (SetProcessing signature)

### Resumo de impacto

| Categoria | Arquivos alterados | Arquivos novos | Risco geral |
|-----------|-------------------|----------------|-------------|
| Trace Details | 3 (router, docker-compose, traces page) | 4 (tempo pkg, handler, db query, frontend page) | Baixo |
| Request Inspector | 2 (router, go.mod) | 2 (handler, db query) | Baixo-Médio |
| Runtime Topology | 1 (router) | 2 (handler, frontend page) | Baixo |
| Worker Details | 4 (worker db, processor, worker main, router) | 4 (migration, handler, db queries, frontend page) | Médio |

**Total: 10 arquivos alterados, 12 arquivos novos.**
