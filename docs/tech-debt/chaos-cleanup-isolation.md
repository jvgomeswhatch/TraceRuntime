# Chaos — Isolamento do Cleanup (prefixo chaos-)

## Status: Implementado (solução temporária)

## Contexto

O cleanup do chaos (`FailStuckTasks` e `FailAbandonedTasks`) originalmente fazia UPDATE global em todas as tasks `processing` ou `pending`, sem distinguir tasks de teste de tasks legítimas da aplicação.

## Solução atual

As funções filtram por `input_payload LIKE 'chaos-%'`, garantindo que apenas tasks criadas pelos cenários de chaos sejam afetadas pelo cleanup.

```sql
-- FailStuckTasks
WHERE status = 'processing' AND input_payload LIKE 'chaos-%'

-- FailAbandonedTasks
WHERE status = 'pending' AND processing_started_at IS NULL AND input_payload LIKE 'chaos-%'
```

Todos os cenários usam prefixo `chaos-` no input:
- `chaos-queue-flood-task-*`
- `chaos-ai-failure-task-*`
- `chaos-slow-inference-task-*`
- `chaos-worker-crash-test`
- `chaos-runtime-hang-test`
- `chaos-postgres-reconnect-test`

## Limitação

O mecanismo depende de convenção de nomenclatura. O Chaos Runner executa cenários sequencialmente, então o prefixo é suficiente para identificar quais tasks pertencem ao chaos como um todo. Porém, não distingue entre cenários individuais.

## Evolução recomendada

Se futuramente for suportada execução paralela de cenários, substituir o prefixo por um identificador explícito:

- Coluna `task_origin` ou `run_id` na tabela `tasks`
- Cada cenário registra seu `run_id` ao criar tasks
- Cleanup filtra por `run_id` em vez de prefixo de payload

## Política de cleanup vs produção

- **Chaos**: abandoned → `failed` durante cleanup. Terminal, sem reprocessamento.
- **Produção**: abandoned apenas sinaliza um incidente operacional via healing event (watchdog). A decisão de recuperação (requeue, replay, intervenção manual) fica para a camada de controle operacional, nunca automaticamente pelo watchdog.

## Arquivos relacionados

- `cmd/chaos/internal/chaos/checker/checker.go` — `FailStuckTasks()`, `FailAbandonedTasks()`
- `cmd/chaos/internal/chaos/scenarios/helpers.go` — `stabilizeSystem()`
- `services/watchdog/internal/detector/detector.go` — `detectAbandonedTasks()` (observação only)
