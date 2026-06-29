# Runbooks / Playbooks Operacionais

## Status: Ausente

## Descrição

O diretório `docs/runbooks/` existe mas contém apenas `.gitkeep`. Não há documentação de resposta a incidentes, troubleshooting, ou procedimentos operacionais.

## Impacto

- Chaos tests validam detecção e recovery, mas não documentam o procedimento humano
- Sem guia de "o que fazer quando X acontece"
- Conhecimento operacional está implícito nos chaos scenarios e no código do watchdog

## O que falta

- [ ] Runbook: worker stale/down (diagnóstico, restart, validação)
- [ ] Runbook: queue lag (backpressure, drain, scaling)
- [ ] Runbook: DLQ não vazia (inspeção, reprocessamento, descarte)
- [ ] Runbook: task stuck (identificação, requeue manual, investigação)
- [ ] Runbook: postgres failure (recovery, backup, verificação de integridade)
- [ ] Runbook: ai-runtime degradado (timeout, fallback, restart Ollama)

## Esforço estimado

Médio — 2-3 dias. A informação já existe nos chaos tests e no código, precisa ser extraída e documentada.

## Origem

`docs/runbooks/` criado como placeholder. Os chaos tests (Phase 9B) contêm implicitamente os procedimentos, mas não estão documentados como runbooks.
