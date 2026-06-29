# Auto-Recovery (Watchdog Actions)

## Status: Detection-only

## Descrição

O watchdog detecta 6 tipos de anomalia (worker.stale, worker.down, queue.lag, task.stuck, dlq.nonempty, clock drift) com deduplicação idempotente e reconciliação no restart. Porém, é explicitamente um observer — não toma ações de recovery.

Comentário no código (`services/watchdog/internal/detector/detector.go`):
> "Detector is the core detection engine. It polls for anomalies and emits healing events. It is an observer only — it does NOT restart workers, requeue tasks, or throttle anything."

## Impacto

- Anomalias são detectadas mas requerem intervenção manual
- Recovery depende de mecanismos indiretos (SQS visibility timeout para redelivery, Docker restart policy)
- Healing events são informativos, não acionáveis automaticamente

## O que falta

- [ ] Requeue automático de tasks stuck via `ChangeMessageVisibility`
- [ ] Restart de containers via Docker API (requer socket mount)
- [ ] Throttling automático quando queue lag excede threshold
- [ ] Escalation policy (detection → warning → action → alert)

## Esforço estimado

Médio-Alto — 3-5 dias. Docker socket mount tem implicações de segurança. Requeue é mais simples (~1 dia).

## Origem

Phase 8 definiu auto-healing como escopo, mas a implementação parou na detecção. Recovery automático foi considerado arriscado sem chaos testing completo (Phase 9B), que já foi concluído.
