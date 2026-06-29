# Worker Details

## Status: Ausente

## Prioridade: Média

## Descrição

Página de detalhe do worker estilo Datadog. Mostra tudo sobre um worker individual em tempo real.

```
worker-1                              ● Healthy

┌─ Vitals ───────────────────────────────────┐
│ uptime          2h 34m                     │
│ heartbeat       3s ago                     │
│ tasks processed 142                        │
│ tasks failed    3                          │
│ avg latency     31.2s                      │
│ goroutines      12                         │
│ model           qwen2.5:3b                 │
└────────────────────────────────────────────┘

┌─ Current Task ─────────────────────────────┐
│ task_id: abc-123                           │
│ processing for: 12s                        │
│ payload: "explain distributed tracing..."  │
└────────────────────────────────────────────┘

┌─ Recent Activity ─────────────────────────┐
│ 14:32:01  task.completed  abc-122  31.2s  │
│ 14:31:30  task.processing abc-123         │
│ 14:30:45  task.completed  abc-121  28.9s  │
└────────────────────────────────────────────┘

┌─ Healing Events ──────────────────────────┐
│ (none active)                             │
│ 13:15:00  worker.stale  resolved  3m ago  │
└────────────────────────────────────────────┘
```

## Dados necessários

A maioria já existe no banco:

- [ ] Frontend: página `/workers/{worker_id}`
- [ ] `worker_heartbeats` table: uptime, tasks_processed, tasks_failed, goroutines, current_task_id, last_seen_at
- [ ] `healing_events` table filtrada por worker_id
- [ ] `tasks` table filtrada por worker (requer associar task ao worker — pode precisar de campo adicional)
- [ ] API: endpoint `/api/workers/{worker_id}` agregando heartbeat + healing events + recent tasks

## Impacto

Transforma o worker de uma caixa preta em algo inspecionável. Em um cenário de chaos testing ou debugging, é onde você vai primeiro.

## Esforço estimado

Médio — 2-3 dias. Dados já estão no PostgreSQL, é query + frontend.
