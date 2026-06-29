# Alerting Rules (Prometheus)

## Status: Ausente

## Descrição

Prometheus está configurado com scrape targets para todos os serviços (otel-collector, api, worker, ai-runtime) mas não possui nenhuma alert rule definida. O watchdog detecta anomalias e publica via SSE, mas não há alertas formais no Prometheus/Alertmanager.

## Impacto

- Anomalias só são visíveis no dashboard (requer alguém olhando)
- Sem notificação proativa (email, Slack, PagerDuty)
- Métricas são coletadas mas nunca geram ação automática
- SLOs definidos nos chaos tests não são monitorados continuamente

## O que falta

- [ ] Alert rules no Prometheus (`prometheus/rules/`)
- [ ] AlertManager configurado (routing, receivers)
- [ ] Regras mínimas: error rate > threshold, queue depth > 50, DLQ > 0, worker stale, p95 latency degradation
- [ ] Integração com canal de notificação (Slack webhook, email)

## Esforço estimado

Baixo — 1 dia. As métricas já existem, é só definir thresholds e receivers.

## Origem

Prometheus foi adicionado na Phase 5 para coleta. Alerting nunca foi escopo de nenhuma fase.
