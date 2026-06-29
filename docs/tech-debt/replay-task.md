# Replay Task

## Status: Ausente

## Prioridade: Média

## Descrição

Funcionalidade extremamente comum em plataformas orientadas a eventos. Permite reexecutar uma task a partir do artefato original (S3) ou do payload original.

```
Task abc-123    Status: completed    Duration: 31.2s

┌─ Output ───────────────────────────────────────────┐
│ "Distributed tracing is a method used to..."       │
└────────────────────────────────────────────────────┘

┌─ Actions ──────────────────────────────────────────┐
│                                                    │
│  [↻ Replay from payload]   [↻ Replay from artifact]│
│                                                    │
│  Replay from payload:                              │
│    Cria nova task com o mesmo input original        │
│    Gera novo trace_id                              │
│    Entra na fila normalmente                       │
│                                                    │
│  Replay from artifact:                             │
│    Busca o artifact do S3                          │
│    Reenvia o payload extraído como nova task       │
│    Útil quando o input original não está mais      │
│    disponível no corpo da task                     │
└────────────────────────────────────────────────────┘
```

## Dados necessários

- [ ] API: endpoint `POST /tasks/{task_id}/replay` que cria nova task com mesmo payload
- [ ] API: lógica para buscar payload do S3 artifact quando replay from artifact
- [ ] Frontend: botões de replay na página de task detail
- [ ] Novo trace_id gerado para o replay (nunca reutilizar trace_id)
- [ ] Link entre task original e task replay (campo `replayed_from` opcional)

## Impacto

Replay é essencial para debugging e validação. "Essa task falhou com timeout, o Ollama tava lento. Replay." Sem isso, o usuário precisa manualmente recriar o payload e submeter via API.

## Esforço estimado

Baixo-Médio — 2 dias. POST /tasks já existe, é estender com lookup de payload + frontend.
