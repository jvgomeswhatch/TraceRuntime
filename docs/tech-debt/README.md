# Technical Debt Registry

Funcionalidades que faltam para fechar o ciclo de operação, investigação e recuperação do runtime.
Organizadas em duas fases: investigação (Phase 10) e controle operacional (Phase 11).

Todas as features devem ser testáveis na CI (integration ou E2E).

---

## Phase 10 — Operational Investigation

| Arquivo | Feature | Prioridade |
|---------|---------|------------|
| [trace-details.md](trace-details.md) | Trace Details | Alta |
| [request-inspector.md](request-inspector.md) | Request Inspector | Alta |
| [runtime-topology.md](runtime-topology.md) | Runtime Topology | Alta |
| [worker-details.md](worker-details.md) | Worker Details | Média |

## Phase 11 — Operational Control

| Arquivo | Feature | Prioridade |
|---------|---------|------------|
| [alert-center.md](alert-center.md) | Alert Center | Alta |
| [replay-task.md](replay-task.md) | Replay Task | Média |
| [dlq-explorer.md](dlq-explorer.md) | DLQ Explorer | Média |
| [chaos-dashboard.md](chaos-dashboard.md) | Chaos Dashboard | Média |

## Infraestrutura (suporte)

| Arquivo | Categoria | Prioridade |
|---------|-----------|------------|
| [alerting-rules.md](alerting-rules.md) | Observabilidade | Alta |
| [auto-recovery.md](auto-recovery.md) | Resiliência | Média |
| [runbooks.md](runbooks.md) | Operações | Média |

---

## Removidos do backlog

- ~~CD Pipeline~~ — CI já cobre. CD não agrega para projeto local.
- ~~Multi-Provider LLM~~ — O projeto é sobre runtime, não sobre providers.
- ~~Horizontal Scaling~~ — Docker Compose, 14GB, sem Kubernetes.
- ~~Secrets Management~~ — Só faria sentido em produção.
- ~~.env.example~~ — Escopo mínimo.
