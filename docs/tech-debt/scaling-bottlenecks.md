# Scaling Bottlenecks — Gargalos para Alto Tráfego

## Contexto

O sistema foi projetado para execução local com ~14GB RAM e Docker Compose.
Os testes de chaos (queue-flood com 60 tasks simultâneas) expuseram gargalos
que não afetam operação local, mas impediriam escalar para tráfego real de produção.

## Gargalos identificados

### 1. Pool de conexões PostgreSQL (pgx)

- **Problema:** O pool default do pgx tem ~4-10 conexões. Sob carga concorrente
  (60+ requests simultâneos), as goroutines competem pelo pool e ficam bloqueadas
  esperando conexão disponível.
- **Impacto:** Latência cresce exponencialmente, timeouts em cascata.
- **Solução futura:** Tunar `MaxConns`, `MinConns`, `MaxConnIdleTime` no pgxpool.
  Considerar PgBouncer como connection pooler externo.

### 2. LocalStack SQS (single-threaded)

- **Problema:** LocalStack Community é single-threaded para SQS. Não suporta
  throughput comparável ao SQS real da AWS.
- **Impacto:** SendMessage fica lento sob carga, causando timeout no producer.
- **Solução futura:** Em produção, usar SQS real da AWS. Para testes locais de
  carga, aceitar a limitação ou usar ElasticMQ como alternativa mais performante.

### 3. Ollama — inferência sequencial

- **Problema:** Ollama processa 1 request por vez (sem batching nativo). Cada
  task precisa esperar a anterior terminar. Com modelo qwen2.5:3b em CPU,
  cada inferência leva 10-60s.
- **Impacto:** Throughput máximo de ~1-6 tasks/minuto. Queue lag cresce
  linearmente com o número de tasks pendentes.
- **Solução futura:**
  - Múltiplas instâncias Ollama (horizontal) com load balancer
  - GPU para reduzir latência de inferência em 10-50x
  - API externa (OpenAI, Anthropic) para throughput elástico
  - Batching no ai-runtime se o modelo suportar

### 4. Worker único

- **Problema:** Apenas 1 worker consome da fila SQS. Mesmo com goroutines
  internas, o gargalo é a inferência sequencial no Ollama.
- **Impacto:** Não há paralelismo real no processamento de tasks.
- **Solução futura:** Múltiplos workers (replicas no Compose ou Kubernetes),
  cada um com sua própria conexão ao SQS. O SQS real suporta consumers
  concorrentes nativamente.

### 5. API sem rate limiting

- **Problema:** Nenhum rate limiting no POST /tasks. Um cliente mal-comportado
  pode inundar a fila e degradar o sistema inteiro.
- **Impacto:** Sem proteção contra flood, DDoS acidental, ou loops de retry.
- **Solução futura:** Rate limiter por IP ou por token no middleware Go
  (golang.org/x/time/rate ou similar). Considerar circuit breaker no producer
  quando queue depth exceder threshold.

## Prioridade

Baixa — o sistema é local-first por design. Esses gargalos só se tornam
relevantes em migração para produção ou em testes de carga reais.

A fundação de observabilidade (watchdog, healing events, chaos testing) já
fornece visibilidade sobre todos esses pontos. O watchdog detecta queue lag,
worker stale e task stuck — exatamente os sintomas que aparecem quando
esses gargalos são atingidos.

## Referências

- Chaos test `queue-flood`: valida detecção de queue lag sob carga
- Watchdog threshold dinâmico: [watchdog-dynamic-threshold.md](watchdog-dynamic-threshold.md)
