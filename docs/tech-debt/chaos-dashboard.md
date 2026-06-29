# Chaos Dashboard

## Status: Backend implementado, frontend ausente

## Prioridade: Média

## Descrição

O framework de chaos testing (`cmd/chaos/`) já existe com 6 cenários validados. Falta expor isso no frontend como painel de operações.

```
Chaos Operations                           ⚠ Use with caution

┌────────────────────────────────────────────────────────────┐
│ Scenario          │ Description              │ Action      │
├───────────────────┼──────────────────────────┼─────────────┤
│ Worker Crash      │ Kill worker with SIGKILL │ [Execute]   │
│ Pause Runtime     │ Pause AI Runtime         │ [Execute]   │
│ AI Failure        │ AI returns 503           │ [Execute]   │
│ Queue Flood       │ Inject 100 messages      │ [Execute]   │
│ Slow Inference    │ Add 10s latency          │ [Execute]   │
│ Postgres Failure  │ Stop PostgreSQL          │ [Execute]   │
└───────────────────┴──────────────────────────┴─────────────┘

Last Run: worker-crash
┌─ Result ───────────────────────────────────────────────────┐
│ Status: PASS                                              │
│ Duration: 61s                                             │
│ Detection: worker.stale detected in 3s                    │
│ Recovery: worker restarted, heartbeat resumed             │
│ SLO: ✅ detection < 15s  ✅ recovery < 120s               │
└───────────────────────────────────────────────────────────┘
```

## Dados necessários

- [ ] API: endpoints para triggerar cenários de chaos (requer CHAOS_ENABLED=true)
- [ ] API: endpoint para listar resultados de chaos runs (`results/` directory)
- [ ] Frontend: página `/chaos` com grid de cenários e botões de execução
- [ ] Frontend: exibição de resultados com SLO validation
- [ ] Proteção: confirmação antes de executar, indicador visual de cenário ativo

## Impacto

Deixa qualquer demonstração muito mais forte. Em vez de rodar `make chaos SCENARIO=worker-crash` no terminal, o avaliador clica um botão e vê o sistema detectar e se recuperar em tempo real.

## Esforço estimado

Médio — 3-4 dias. Backend de chaos existe, precisa de API wrapper + frontend.
