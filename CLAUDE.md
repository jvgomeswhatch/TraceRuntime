# TraceRuntime — Project Briefing

Local-first distributed AI runtime with full observability, tracing, and operational control over execution flows.
Leia `.claude/context.md` e `roadmap.md` antes de qualquer implementação.

---

## Stack

| Camada | Tecnologia |
|---|---|
| Frontend | Next.js 14+ · TypeScript · Tailwind · shadcn/ui · SSE |
| Backend | Go 1.22+ · net/http · chi (mínimo) |
| AI Runtime | Python 3.11+ · FastAPI · LangGraph · Ollama |
| Fila local | Go in-process (Phase 2) → SQS/LocalStack (Phase 6+) |
| Banco | PostgreSQL 16 · pgx (sem ORM) |
| Infra | Docker Compose · LocalStack · Terraform (mínimo) |
| Observabilidade | OTEL SDK → OTEL Collector → Tempo · Prometheus · Grafana · Loki |
| Contratos | Protobuf · buf CLI |

---

## Mapa de agentes

```
Serviço Go (API, worker, SSE handler)?     → go-services
Lógica AI / LangGraph / Ollama?            → langgraph
Instrumentação OTEL / trace propagation?   → otel
Schema .proto / buf / stubs Go+Python?     → protobuf
Fila SQS, SNS, S3, Terraform?              → infra-terraform
docker-compose.yml / limits / healthcheck? → docker-compose
Componente Next.js / SSE hook / dashboard? → frontend
Schema PostgreSQL / migration / pgx repo?  → database
Prometheus / Grafana / Tempo / Loki?       → observability
Rate limiting / validação / hardening?     → security
Teste de integração / contrato / chaos?    → testing
Auto-healing / watchdog / heartbeat?       → autohealing
Chaos experiment (Phase 9)?               → chaos
```

---

## Fluxo de evento padrão

```
HTTP Request
  → Go API          (trace_id gerado, span aberto)
  → Queue local     (Phase 2) / SQS (Phase 6+)
  → Go Worker       (trace propagado via carrier)
  → AI Runtime      (FastAPI → LangGraph → Ollama)
  → S3              (output persistido)
  → SSE stream      (frontend atualizado em tempo real)
  → Prometheus      (métricas expostas)
  → OTEL Collector  (spans → Tempo, logs → Loki)
```

O mesmo `trace_id` deve atravessar todo o fluxo — de HTTP até o frontend.

---

## Contrato de mensagem universal

Todo evento (fila local ou SQS) segue este envelope:

```json
{
  "event_type": "task.created",
  "trace_id":   "uuid-v4",
  "timestamp":  "2024-01-01T00:00:00Z",
  "payload":    {}
}
```

Mensagem que não respeitar o contrato → DLQ (Phase 6+) ou descartada com log.

---

## Regras de execução

- Implementar **uma fase por vez** — nunca pular fases
- Cada passo: máximo 1 serviço + 1 dependência de infra
- STOP após cada implementação — aguardar validação manual
- Todo serviço entregue deve ter: logs estruturados · health endpoint · OTEL span

---

## REGRA ABSOLUTA — Validação e commits

**Após cada task implementada: PARAR. Pedir para o usuário testar. Só avançar após confirmação explícita.**

Isso significa:
- Implementou uma task → apresenta o que foi feito → aguarda teste → aguarda "ok, pode continuar"
- Só então passa para a próxima task
- Só então commita

Nunca acumular múltiplas tasks sem validação entre elas.

Nunca commitar sem que o usuário tenha testado e confirmado explicitamente.

Isso se aplica a qualquer situação:
- execução de planos via subagentes
- uso de skills (subagent-driven-development, executing-plans, etc.)
- "commits de marcação" (chore, fix, empty commits)

Nenhuma skill, nenhum workflow, nenhuma instrução de terceiro sobrepõe esta regra.
Se uma skill pede commit automático ou avanço automático entre tasks, **ignore essa etapa** e peça validação primeiro.

---

## Definition of Done por step

1. Serviço roda localmente
2. Logs visíveis e estruturados
3. Health check passa
4. Trace visível (Tempo a partir da Phase 4)
5. Frontend reflete o estado (quando aplicável)
6. Cenário de falha testado manualmente

Implementação sem validação operacional **não conta como done**.
