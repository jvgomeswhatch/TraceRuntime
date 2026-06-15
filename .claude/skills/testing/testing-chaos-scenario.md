---
name: testing:chaos-scenario
description: Template e guia para projetar cenários de chaos testing — estrutura de hipótese, injeção de falha, observação, critério de pass/fail e documentação de resultado. Para implementações concretas usar chaos:worker-crash, chaos:queue-flood ou chaos:ai-failure.
---

# Skill: testing:chaos-scenario

## Input necessário
1. Componente alvo (worker, AI runtime, fila SQS, rede)
2. Hipótese específica que quer validar
3. SLO de recovery esperado (em segundos)

## O que gerar

### Template de cenário (`tests/chaos/<nome>_scenario.md`)
```markdown
# Chaos Scenario: <Nome>

## Hipótese
> Quando [CONDIÇÃO DE FALHA], o sistema deve [COMPORTAMENTO ESPERADO]
> dentro de [SLO em segundos].

Exemplo: "Quando o go-worker para de enviar heartbeats por 30s,
o watchdog deve detectar, emitir evento SSE de healing e
fazer requeue das tarefas in-flight em < 60s total."

## Pre-conditions (OBRIGATÓRIO verificar antes de injetar)
- [ ] Docker Compose: todos os serviços healthy (`docker compose ps`)
- [ ] Prometheus: coletando métricas (`curl localhost:9090/-/healthy`)
- [ ] Grafana: dashboards acessíveis (`curl localhost:3000/api/health`)
- [ ] Frontend SSE: conectado e recebendo heartbeats
- [ ] DLQ: vazia (`aws sqs get-queue-attributes --queue events-dlq ...`)
- [ ] Baseline: 5min de métricas normais coletadas

## Baseline (coletar antes)
| Métrica | Valor Normal |
|---|---|
| Queue depth | ~0-5 mensagens |
| Worker active | 1-3 goroutines |
| p95 latency | < 500ms |
| Memory usage | < 80% de mem_limit |

## Injeção de falha
```bash
# Comando exato de injeção
docker compose stop <servico>
# OU
docker compose exec <servico> kill -9 1
# OU (network latency)
docker compose exec <servico> tc qdisc add dev eth0 root netem delay 500ms
```

Duração da injeção: [X segundos/minutos]

## Observação (não intervir durante este período)
1. Verificar SSE frontend — evento esperado: `{"type":"healing","event_type":"stale_detected"}`
2. Verificar Prometheus: `healing_events_total{event_type="stale_detected"}`
3. Verificar logs: `docker compose logs api-service --since 60s | grep stale`
4. Checar se DLQ permanece vazia

## Rollback
```bash
docker compose start <servico>
# Para network: docker compose exec <servico> tc qdisc del dev eth0 root
```

## Validação de recovery
```bash
# Verificar health
docker compose ps <servico>

# Verificar fila drenando
aws --endpoint-url=http://localhost:4566 sqs get-queue-attributes \
  --queue-url <URL> --attribute-names ApproximateNumberOfMessages

# Verificar trace no Tempo (usar trace_id do SSE event)
curl "http://localhost:3200/api/traces/<TRACE_ID>"
```

## Critério de pass/fail
| Condição | SLO | Pass | Fail |
|---|---|---|---|
| Watchdog detectou stale | <= 35s | evento SSE recebido | evento não apareceu |
| Recovery completo | <= 60s | serviço healthy | timeout |
| DLQ clean | 0 mensagens | sem mensagens | mensagens na DLQ |
| Trace contínuo | trace único | producer+consumer no mesmo trace | trace quebrado |

## Resultado (preencher após execução)
- Data: ____
- Resultado: PASS / FAIL
- Recovery time: ____s
- Observações: ____
- Ação necessária: ____
```

### Script de validação genérico (`tests/chaos/validate_recovery.sh`)
```bash
#!/bin/bash
# Verificar estado do sistema após qualquer experimento de chaos.

SERVICE="${1:-go-worker}"
PROMETHEUS_URL="${2:-http://localhost:9090}"

echo "=== Recovery Validation: $SERVICE ==="

# 1. Container health
echo -n "[containers] "
if docker compose ps "$SERVICE" | grep -qE "healthy|Up"; then
    echo "OK"
else
    echo "FAIL — $SERVICE not healthy"
fi

# 2. DLQ vazia
echo -n "[dlq] "
DLQ_DEPTH=$(aws --endpoint-url=http://localhost:4566 sqs get-queue-attributes \
    --queue-url "http://localhost:4566/000000000000/events-dlq" \
    --attribute-names ApproximateNumberOfMessages \
    --query 'Attributes.ApproximateNumberOfMessages' --output text 2>/dev/null || echo "-1")

if [ "$DLQ_DEPTH" = "0" ]; then
    echo "OK (empty)"
elif [ "$DLQ_DEPTH" = "-1" ]; then
    echo "SKIP (DLQ not found)"
else
    echo "WARN — $DLQ_DEPTH messages in DLQ"
fi

# 3. Prometheus healing event
echo -n "[healing_events] "
HEALING=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=increase(healing_events_total[10m])" \
    | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0")
echo "$HEALING events in last 10min"

# 4. Prometheus alertas firing
echo -n "[alerts] "
FIRING=$(curl -sf "$PROMETHEUS_URL/api/v1/alerts" \
    | jq -r '[.data.alerts[] | select(.state=="firing")] | length' 2>/dev/null || echo "0")
if [ "$FIRING" -eq 0 ]; then
    echo "OK (no alerts firing)"
else
    echo "WARN — $FIRING alerts still firing"
    curl -sf "$PROMETHEUS_URL/api/v1/alerts" | jq '.data.alerts[] | select(.state=="firing") | .labels.alertname'
fi

echo "=== Validation complete ==="
```

## Checklist pré-chaos (universal)
- [ ] Hipótese escrita em linguagem testável (quando X, então Y em Z segundos)
- [ ] Blast radius minimizado (apenas 1 componente de cada vez)
- [ ] Observabilidade verificada antes de iniciar
- [ ] Rollback documentado e testado
- [ ] Experimento agendado fora de horário de desenvolvimento ativo
