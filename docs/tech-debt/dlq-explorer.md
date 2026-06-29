# DLQ Explorer

## Status: Ausente

## Prioridade: Média

## Descrição

Hoje o dashboard mostra apenas "DLQ: Empty" ou um número. O DLQ Explorer permite inspecionar, reprocessar ou descartar mensagens da dead letter queue.

```
DLQ Explorer                                    3 messages

┌────────────────────────────────────────────────────────────────┐
│ Message ID    │ Attempts │ Error              │ Actions        │
├───────────────┼──────────┼────────────────────┼────────────────┤
│ msg-abc-123   │ 3/3      │ AI timeout (31s)   │ [Retry] [Del]  │
│ msg-def-456   │ 3/3      │ Invalid payload    │ [Retry] [Del]  │
│ msg-ghi-789   │ 3/3      │ S3 write failed    │ [Retry] [Del]  │
└───────────────┴──────────┴────────────────────┴────────────────┘

Detail (msg-abc-123):
┌─ Payload ──────────────────────────────────────────────────────┐
│ { "task_id": "abc-123",                                       │
│   "trace_id": "trace-xyz",                                    │
│   "payload": "explain distributed tracing in Go" }            │
└────────────────────────────────────────────────────────────────┘

┌─ Attributes ───────────────────────────────────────────────────┐
│ ApproximateReceiveCount: 3                                     │
│ SentTimestamp: 2024-01-15T14:32:00Z                           │
│ traceparent: 00-trace-xyz-span-01                              │
└────────────────────────────────────────────────────────────────┘
```

## Dados necessários

- [ ] API: endpoint `/api/dlq` que lista mensagens via SQS `ReceiveMessage` (sem delete, visibility 0)
- [ ] API: endpoint `/api/dlq/{receipt_handle}/retry` que move mensagem de volta para a fila principal
- [ ] API: endpoint `/api/dlq/{receipt_handle}/delete` que remove mensagem da DLQ
- [ ] Frontend: página `/dlq` com tabela, detail view, ações de retry/delete
- [ ] Parsing do body da mensagem SQS para exibir payload e error

## Impacto

DLQ é onde as falhas vão morrer silenciosamente. O Explorer transforma mensagens mortas em investigáveis e recuperáveis.

## Esforço estimado

Médio — 2-3 dias. SQS API já disponível, é endpoint + frontend.
