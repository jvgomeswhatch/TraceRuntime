# Trace Details

## Status: Ausente

## Prioridade: Alta

## Descrição

Feature obrigatória para fechar o ciclo de observabilidade. Hoje o fluxo Task → Evento → Trace existe, mas não é possível responder "onde os 35 segundos foram gastos?".

Uma página de trace detail mostra a timeline completa de uma task com breakdown por estágio:

```
Task abc123   Status: completed   Latency total: 31.5s

Timeline:
API              3ms    ████
  ↓
Persist DB       5ms    ████
  ↓
SQS             12ms    ████
  ↓
Worker           1ms    ████
  ↓
AI Runtime     120ms    █████
  ↓
LangGraph      250ms    ██████
  ↓
Ollama        31.2s     ████████████████████████████████████████
  ↓
S3             20ms     ████
```

## Dados necessários

Os spans já existem no Tempo. O que falta:

- [ ] Frontend: página `/traces/{trace_id}` com timeline visual
- [ ] API: endpoint para buscar spans de um trace_id no Tempo (ou query via Grafana datasource)
- [ ] Cálculo de duração por estágio a partir dos spans OTEL
- [ ] Visualização waterfall (barras horizontais proporcionais à duração)

## Impacto

Sem isso, o usuário vê que uma task demorou 31s mas não sabe se foi o Ollama, o SQS, ou o Worker. É a diferença entre "ver métricas" e "investigar problemas".

## Esforço estimado

Médio — 3-4 dias. Spans já existem no Tempo, é frontend + query.
