# Runtime Topology

## Status: Ausente

## Prioridade: Alta

## Descrição

Página mostrando a topologia completa do runtime em tempo real. Comunica a arquitetura em segundos e mostra o estado operacional de cada componente.

```
┌─────────┐     ┌─────────┐     ┌─────────┐     ┌──────────┐
│ Client  │────▶│   API   │────▶│   SQS   │────▶│  Worker  │
│         │     │  :8082  │     │ tasks   │     │  :9091   │
│         │     │ ● 3ms   │     │ depth:2 │     │ ● 1ms   │
└─────────┘     └─────────┘     └─────────┘     └──────────┘
                                                      │
                                                      ▼
┌─────────┐     ┌─────────┐     ┌──────────┐    ┌──────────┐
│   S3    │◀────│ Ollama  │◀────│ LangGraph│◀───│AI Runtime│
│ outputs │     │qwen2.5  │     │ classify │    │  :8001   │
│ 142 obj │     │ 3.1t/s  │     │ generate │    │ ● 120ms │
└─────────┘     └─────────┘     │ validate │    └──────────┘
                                └──────────┘

Cada bloco mostra:
  ● status (healthy/degraded/down)
  latência média
  requests/min
  erros recentes
```

## Dados necessários

- [ ] Frontend: página `/topology` com visualização de grafo ou grid
- [ ] API: endpoint `/api/topology` que agrega health + métricas de cada serviço
- [ ] Health status de cada componente (já existe via /health endpoints)
- [ ] Métricas de cada nó (Prometheus queries ou endpoints existentes)
- [ ] Conexões entre nós com indicadores de throughput

## Impacto

É a página que explica o projeto inteiro sem precisar ler documentação. Em uma demo ou entrevista, é a primeira coisa que você abre.

## Esforço estimado

Médio-Alto — 3-5 dias. Health checks já existem, métricas parcialmente. O desafio é a visualização.
