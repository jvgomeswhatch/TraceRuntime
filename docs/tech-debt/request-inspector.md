# Request Inspector

## Status: Ausente

## Prioridade: Alta

## Descrição

DevTools para uma execução distribuída. Ao abrir uma task, o inspector mostra todos os artefatos daquela execução em um único lugar:

```
Task abc123

┌─ Request ──────────────────┐
│ Headers                    │
│   traceparent: 00-abc...   │
│   content-type: app/json   │
│                            │
│ Trace Context              │
│   trace_id: abc123         │
│   span_id: def456          │
│   parent_span_id: —        │
│                            │
│ Payload                    │
│   { "input": "explain..." }│
└────────────────────────────┘

┌─ Response ─────────────────┐
│ Artifact (S3)              │
│   { "output": "...",       │
│     "model": "qwen2.5:3b", │
│     "tokens_per_second": 3.1}│
└────────────────────────────┘

┌─ Logs ─────────────────────┐
│ [api]    task.created       │
│ [worker] task.processing    │
│ [ai]     inference started  │
│ [ai]     inference complete │
│ [worker] task.completed     │
└────────────────────────────┘

┌─ Metrics ──────────────────┐
│ prompt_tokens: 45           │
│ completion_tokens: 120      │
│ tokens_per_second: 3.1      │
│ inference_duration: 31.2s   │
└────────────────────────────┘

┌─ Timeline ─────────────────┐
│ (waterfall do trace-details)│
└────────────────────────────┘
```

## Dados necessários

- [ ] Frontend: página ou modal `/tasks/{task_id}/inspect`
- [ ] API: endpoint que agrega dados de PostgreSQL (task + tokens), S3 (artifact content), Loki (logs filtrados por trace_id), Tempo (spans)
- [ ] Ou: composição no frontend a partir de endpoints existentes + novos

## Impacto

Transforma o dashboard de "visualização de estado" para "ferramenta de investigação". É o equivalente a Chrome DevTools para o runtime distribuído.

## Esforço estimado

Alto — 4-5 dias. Requer aggregação de múltiplas fontes (DB, S3, Loki, Tempo).
