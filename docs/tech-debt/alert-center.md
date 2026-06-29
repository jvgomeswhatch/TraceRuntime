# Alert Center

## Status: Ausente

## Prioridade: Alta

## Descrição

Hoje o dashboard mostra apenas um número de healing events. O Alert Center é uma página clicável que lista todos os alertas com status, histórico e detalhes.

```
Alert Center                              3 active  12 resolved

┌──────────────────────────────────────────────────────────┐
│ ⚠ Worker Down         worker-1    RESOLVED    3m ago    │
│   Detected: 14:15:00  Resolved: 14:18:00  Duration: 3m │
│   Details: heartbeat stale > 60s                        │
├──────────────────────────────────────────────────────────┤
│ 🔴 Queue Lag           —          ACTIVE      now       │
│   Detected: 14:20:00  Threshold: 50  Current: 67       │
│   Details: queue depth exceeds threshold                │
├──────────────────────────────────────────────────────────┤
│ ⚠ High Latency        —          RESOLVED    15m ago   │
│   Detected: 14:05:00  Resolved: 14:10:00  Duration: 5m │
│   Details: p95 latency > 60s (measured: 85s)            │
├──────────────────────────────────────────────────────────┤
│ 🔴 DLQ Non-Empty       —          ACTIVE      now       │
│   Detected: 14:22:00  Messages: 3                      │
│   Details: dead letter queue has unprocessed messages    │
└──────────────────────────────────────────────────────────┘

Filtros: [All] [Active] [Resolved]  |  Tipo: [All] [Worker] [Queue] [Latency]
```

## Dados necessários

Os dados já existem na tabela `healing_events`:

- [ ] Frontend: página `/alerts` com lista filtrável
- [ ] API: endpoint `/api/alerts` (ou expandir `/api/operations/summary`)
- [ ] Filtros: status (active/resolved), event_type, severity, date range
- [ ] Detail view com timeline do evento (created → resolved)
- [ ] Badge no nav com contagem de alertas ativos

## Impacto

Sem isso, anomalias detectadas pelo watchdog são invisíveis a menos que o usuário esteja olhando o dashboard no momento. O Alert Center dá visibilidade histórica e investigação.

## Esforço estimado

Baixo-Médio — 2 dias. Dados existem no PostgreSQL, é basicamente frontend + query.
