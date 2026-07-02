# Watchdog — Dynamic Queue Lag Threshold

## Status: Backlog

## Contexto

O `WATCHDOG_QUEUE_LAG_THRESHOLD` é atualmente um valor fixo (10) configurado via variável de ambiente. Esse valor foi definido com base no sistema atual (1 worker, processamento sequencial) e é operacionalmente correto para essa configuração.

Durante a implementação dos cenários de chaos testing, identificamos que o threshold anterior (50) era alto demais para a capacidade real do sistema — o worker drenava a fila antes do watchdog amostrar, causando falsos negativos na detecção de queue lag.

## Problema

O threshold fixo não se adapta a mudanças de capacidade:
- Se o número de workers aumentar, o threshold atual pode gerar alertas falsos (a fila drena mais rápido)
- Se o throughput do worker mudar (modelo diferente, payload maior), o threshold pode ficar defasado
- Não existe relação formal entre o threshold e a capacidade medida do sistema

## Proposta

Derivar o threshold a partir da capacidade do sistema em vez de mantê-lo como constante:

```
threshold = worker_count * avg_processing_rate * acceptable_lag_seconds
```

Exemplo: 1 worker processando ~1 task/10s com lag aceitável de 60s → threshold = 6.

### Opções de implementação

1. **Estático calculado**: Fórmula no config com inputs explícitos (worker_count, rate). Simples, requer reconfiguração manual.
2. **Dinâmico observado**: Watchdog mede throughput real nas últimas N amostras e ajusta o threshold. Mais complexo, auto-adaptativo.

## Decisão atual

Manter o valor fixo de 10 até que o sistema escale (mais workers ou mudança significativa de throughput). Reavaliar quando houver necessidade concreta.

## Arquivos relacionados

- `docker-compose.yml` — `WATCHDOG_QUEUE_LAG_THRESHOLD=10`
- `services/watchdog/internal/config/config.go` — parsing e validação
- `services/watchdog/internal/detector/detector.go` — `detectQueueLag()`
