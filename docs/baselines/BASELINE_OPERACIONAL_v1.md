# BASELINE OPERACIONAL v1

Baseline gerado em 2026-06-22 com inferência real Ollama.

## Hardware

| Recurso | Valor |
|---|---|
| CPU | AMD Ryzen 5 3500U (4 cores / 8 threads) |
| RAM | 14 GB (sem GPU) |
| OS | Windows 11 Pro + Docker Desktop (WSL2) |
| Docker Total | ~14 GB compartilhado entre todos os containers |

## Stack de Teste

| Componente | Configuração |
|---|---|
| Modelo | qwen2.5:3b (Ollama) |
| Worker | 1 instância, concurrency=1 |
| Fila | SQS (LocalStack), VT=360s, DLQ maxReceiveCount=3 |
| DB | PostgreSQL 16-alpine, 256MB |
| Loadtest | cmd/loadtest, rate=0.5 task/s |

## Métricas de Inferência

Dados do teste de 5 tasks com pipeline completo (API → SQS → Worker → AI Runtime → S3 → DB).

### Token Throughput

| Métrica | Valor |
|---|---|
| Avg tokens/s | 2.96 |
| p50 tokens/s | 2.90 |
| p95 tokens/s | 3.30 |
| Min tokens/s | 2.72 |
| Max tokens/s | 3.34 |

Ceiling de hardware: ~3.3 tokens/s para qwen2.5:3b no Ryzen 5 3500U sem GPU.

### Output Tokens por Task

| Métrica | Valor |
|---|---|
| p50 | 443 tokens |
| p95 | 656 tokens |
| Max | 667 tokens |
| Min | 66 tokens |

### Prompt Tokens

Média: ~39 tokens por request (inputs curtos de loadtest).

## Latência de Processamento

### Processing Duration (inferência + S3 write)

| Métrica | Valor |
|---|---|
| p50 | 167s |
| p95 | 286s |
| p99 | 297s |
| Min | 21s |
| Max | 300s (timeout) |

Variância alta é diretamente proporcional ao output token count:
- 66 tokens → 21s
- 290 tokens → 98s
- 595 tokens → 227s
- 667 tokens → 167s (2.72 tok/s, consistente)

### Queue Wait (submit → processing start)

| Métrica | Valor |
|---|---|
| p50 | 194s |
| p95 | 471s |
| Max | 516s |

Queue wait alto é esperado com 1 worker serial: tasks esperam as anteriores completarem.

### End-to-End (submit → completed)

| Métrica | Valor |
|---|---|
| p50 | 292s |
| p95 | 756s |
| Max | 816s |

## Parâmetros Calibrados

### VisibilityTimeout

| Parâmetro | Valor | Justificativa |
|---|---|---|
| Anterior | 240s (provisional) | Insuficiente — p95 processing = 326s no teste de 10 tasks |
| Atual | 360s | p95 processing (286s) + 74s margem |
| Fórmula | `p95_processing + 30s` | 286 + 30 = 316, arredondado para 360 por segurança |

### Inference Deadline (callAIRuntime)

| Parâmetro | Valor | Observação |
|---|---|---|
| Atual | 300s | 1 task falhou por timeout com output grande |
| Recomendado | 360s | Alinhar com VT para evitar timeout antes do redelivery |

### Queue Max Depth

| Parâmetro | Valor |
|---|---|
| Threshold | 50 |
| Peak observado | 9-10 messages |
| Status | Adequado |

### Worker Concurrency

| Parâmetro | Valor |
|---|---|
| Atual | 1 worker, 1 goroutine |
| Recomendação | Manter 1 até validar com 25+ tasks |

## Incidentes Resolvidos

### Clock Drift (Docker/WSL2)

**Problema:** `processing_started_at > completed_at` em tasks completadas com sucesso — durações negativas (-48s a -67s).

**Causa raiz:** Docker Desktop no WSL2 tem clock drift. O `time.Now()` do Go dentro do container retorna timestamps não-monotônicos. Comprovado por logs do worker com timestamp de "task completed" anterior ao "processing task" no mesmo processo serial.

**Fix:** Migração de `time.Now().UTC()` para `NOW()` do PostgreSQL em `SetProcessing`, `SetCompleted`, `SetFailed`. Clock único e estável.

**Validação:** 5/5 tasks com timestamps monotonicamente corretos após o fix. `negative_durations = 0`.

### Fixes Defensivos

1. `COALESCE(processing_started_at, NOW())` em SetProcessing — previne sobrescrita
2. Clamp de durações negativas no collector com counter `negative_durations` — anomalia permanece observável no report JSON

## Limitações Conhecidas

1. **Amostra pequena:** 5 tasks (10 submetidas, 5 falharam na submission por API reiniciando). Baseline deve ser consolidado com 10-25 tasks estáveis.
2. **1 worker serial:** Queue wait cresce linearmente com backlog. Não testado com concurrency > 1.
3. **Inference deadline < VT:** 300s < 360s pode causar falha de tasks com output > 600 tokens.
4. **Hardware-bounded:** ~3.3 tok/s é ceiling do Ryzen 5 3500U. Não há otimização de software que melhore isso.

## Grafana Dashboards

- **LLM Capacity:** 7 painéis (tokens/s, prompt/completion rates, output distribution, inference speed, cumulative tokens)
- **Prometheus metrics:** `traceruntime_llm_*` (5 métricas com labels `model`, `task_type`)

## Próximos Passos (9B)

Chaos testing com estes baselines como referência:
- Worker crash durante inferência → task deve ir para DLQ após maxReceiveCount=3
- AI Runtime failure → worker deve falhar gracefully, task marcada failed
- Queue flood (50+ tasks) → admission control deve rejeitar, backlog não deve crescer indefinidamente
- Latency spike → watchdog deve detectar stuck tasks > 300s
