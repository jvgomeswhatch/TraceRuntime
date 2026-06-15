---
name: chaos:queue-flood
description: Experimento de chaos que injeta volume alto de mensagens na fila SQS (LocalStack) para validar backpressure do worker, alertas de queue lag no Prometheus e ausência de crash por OOM. Worker deve processar sem crashar, lag não deve crescer indefinidamente.
---

# Skill: chaos:queue-flood

## Input necessário
1. Nome da fila SQS (ex: `events-queue`)
2. Número de mensagens a injetar (default: 500 — suficiente para lag visível)
3. Rate de injeção (default: 50 msg/s — rápido o suficiente para criar backlog)

## O que gerar

### `tests/chaos/queue_flood_test.sh`
```bash
#!/bin/bash
set -euo pipefail

# Experimento: Queue Flood
# Hipótese: Ao injetar 500 mensagens a 50 msg/s, o worker processa com backpressure
#           controlado (sem crash, sem OOM), lag alerta no Prometheus em < 60s,
#           e fila drena completamente em < 5min após flood.
#
# Não testa recovery de crash — testa estabilidade sob carga.

QUEUE_NAME="${1:-events-queue}"
FLOOD_COUNT="${2:-500}"
FLOOD_RATE="${3:-50}"    # mensagens por segundo
LOCALSTACK_URL="http://localhost:4566"
PROMETHEUS_URL="http://localhost:9090"
API_URL="http://localhost:8080"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log() { echo -e "${YELLOW}[$(date +%T)] $1${NC}"; }
pass() { echo -e "${GREEN}PASS: $1${NC}"; }
fail() { echo -e "${RED}FAIL: $1${NC}"; exit 1; }

# Obter URL da fila
QUEUE_URL=$(aws --endpoint-url="$LOCALSTACK_URL" sqs get-queue-url \
    --queue-name "$QUEUE_NAME" \
    --query 'QueueUrl' --output text 2>/dev/null) || \
    fail "Queue $QUEUE_NAME not found in LocalStack"

# 1. PRE-CONDITIONS
log "=== [1/5] Pre-conditions ==="

INITIAL_DEPTH=$(aws --endpoint-url="$LOCALSTACK_URL" sqs get-queue-attributes \
    --queue-url "$QUEUE_URL" \
    --attribute-names ApproximateNumberOfMessages \
    --query 'Attributes.ApproximateNumberOfMessages' --output text)

log "Queue $QUEUE_NAME — Initial depth: $INITIAL_DEPTH"

WORKER_MEM_BEFORE=$(docker stats go-worker --no-stream --format "{{.MemUsage}}" 2>/dev/null || echo "N/A")
log "Worker memory before flood: $WORKER_MEM_BEFORE"

# 2. INJECT FLOOD
log "=== [2/5] Injecting $FLOOD_COUNT messages at ~$FLOOD_RATE msg/s ==="
FLOOD_START=$(date +%s)

flood_queue() {
    local count=$1
    local rate=$2
    local delay
    delay=$(echo "scale=3; 1/$rate" | bc)

    for i in $(seq 1 "$count"); do
        TRACE_ID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen)
        aws --endpoint-url="$LOCALSTACK_URL" sqs send-message \
            --queue-url "$QUEUE_URL" \
            --message-body "{\"event_type\":\"chaos.flood\",\"trace_id\":\"$TRACE_ID\",\"seq\":$i}" \
            > /dev/null

        if (( i % 50 == 0 )); then
            log "Sent $i/$count messages..."
        fi
        sleep "$delay"
    done
}

flood_queue "$FLOOD_COUNT" "$FLOOD_RATE"
log "Flood complete. Monitoring backpressure..."

# 3. MONITOR (sem intervir — observar estabilização)
log "=== [3/5] Monitoring for 120s ==="

WORKER_CRASHED=false
LAG_ALERTED=false
MAX_LAG=0

for i in $(seq 1 24); do  # 24 * 5s = 120s
    sleep 5

    # Verificar se worker ainda está rodando
    if ! docker compose ps go-worker 2>/dev/null | grep -qE "Up|healthy"; then
        WORKER_CRASHED=true
        log "CRITICAL: Worker crashed at $((i * 5))s into flood!"
        break
    fi

    # Queue lag atual
    CURRENT_LAG=$(aws --endpoint-url="$LOCALSTACK_URL" sqs get-queue-attributes \
        --queue-url "$QUEUE_URL" \
        --attribute-names ApproximateNumberOfMessages \
        --query 'Attributes.ApproximateNumberOfMessages' --output text 2>/dev/null || echo "0")

    if (( CURRENT_LAG > MAX_LAG )); then
        MAX_LAG=$CURRENT_LAG
    fi

    # Verificar alerta Prometheus
    ALERT=$(curl -sf \
        "$PROMETHEUS_URL/api/v1/query?query=ALERTS{alertname='QueueLagHigh',state='firing'}" \
        | jq -r '.data.result | length' 2>/dev/null || echo "0")

    if [ "$ALERT" -gt 0 ] && [ "$LAG_ALERTED" = false ]; then
        LAG_ALERTED=true
        log "Prometheus alert QueueLagHigh fired at $((i * 5))s"
    fi

    if (( i % 4 == 0 )); then
        WORKER_MEM=$(docker stats go-worker --no-stream --format "{{.MemUsage}}" 2>/dev/null || echo "N/A")
        log "t+$((i * 5))s — Queue lag: $CURRENT_LAG, Worker mem: $WORKER_MEM"
    fi
done

# 4. WAIT FOR DRAIN
log "=== [4/5] Waiting for queue to drain (max 5min) ==="
DRAIN_TIMEOUT=300
DRAINED=false

for i in $(seq 1 60); do  # 60 * 5s = 300s
    sleep 5
    REMAINING=$(aws --endpoint-url="$LOCALSTACK_URL" sqs get-queue-attributes \
        --queue-url "$QUEUE_URL" \
        --attribute-names ApproximateNumberOfMessages \
        --query 'Attributes.ApproximateNumberOfMessages' --output text 2>/dev/null || echo "-1")

    if [ "$REMAINING" -eq 0 ]; then
        DRAIN_TIME=$(($(date +%s) - FLOOD_START))
        log "Queue drained in ${DRAIN_TIME}s"
        DRAINED=true
        break
    fi

    if (( i % 6 == 0 )); then
        log "Draining... $REMAINING messages remaining"
    fi
done

# 5. PASS/FAIL
log "=== [5/5] Results ==="
TOTAL_TIME=$(($(date +%s) - FLOOD_START))
WORKER_MEM_AFTER=$(docker stats go-worker --no-stream --format "{{.MemUsage}}" 2>/dev/null || echo "N/A")

echo "Total time: ${TOTAL_TIME}s"
echo "Max queue depth observed: $MAX_LAG"
echo "Worker memory: before=$WORKER_MEM_BEFORE → after=$WORKER_MEM_AFTER"
echo "Prometheus lag alert fired: $LAG_ALERTED"
echo "Queue drained: $DRAINED"

PASSED=true

if [ "$WORKER_CRASHED" = true ]; then
    log "FAIL: Worker crashed during flood — check OOM or panic"
    PASSED=false
fi

if [ "$DRAINED" = false ]; then
    log "FAIL: Queue not drained in 5min — worker may be stuck"
    PASSED=false
fi

if [ "$PASSED" = true ]; then
    pass "Queue flood experiment passed — max lag $MAX_LAG, drained in ${TOTAL_TIME}s"
else
    fail "Queue flood experiment FAILED"
fi
```

## O que observar durante o experimento
- `worker_pool_active` deve manter-se em `maxWorkers` (saturado, não crashado)
- `container_memory_usage_bytes{name="go-worker"}` não deve ultrapassar `mem_limit`
- Fila cresce, estabiliza, depois drena — sem crescimento infinito
- Sem mensagens na DLQ (processadas com sucesso)

## Checklist pré-experimento
- [ ] LocalStack up com fila criada
- [ ] `aws` CLI configurada para LocalStack (`AWS_ENDPOINT_URL=http://localhost:4566`)
- [ ] Alerta `QueueLagHigh` configurado no Prometheus (> 50 mensagens)
- [ ] Worker com `mem_limit` definido no docker-compose
- [ ] DLQ vazia antes do flood
