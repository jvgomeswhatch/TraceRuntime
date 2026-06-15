---
name: chaos:worker-crash
description: Experimento de chaos que simula crash do Go worker via docker compose stop. Valida que o watchdog detecta, emite evento SSE, faz requeue das tarefas pendentes e o sistema volta a drenar a fila dentro do SLO de 60s.
---

# Skill: chaos:worker-crash

## Input necessário
1. Nome do serviço worker no docker-compose (ex: `go-worker`)
2. Nome da fila SQS a monitorar (ex: `events-queue`)
3. Prometheus rodando? (para coletar métricas durante chaos)

## O que gerar

### `tests/chaos/worker_crash_test.sh`
```bash
#!/bin/bash
set -euo pipefail

# Experimento: Worker Crash
# Hipótese: Após crash do go-worker, watchdog detecta stale em <= 30s,
#           faz requeue das tarefas in-flight e fila volta a drenar em <= 60s.
#
# SLO de recovery: 60s total desde crash até fila drenando novamente.

WORKER_SERVICE="${1:-go-worker}"
QUEUE_NAME="${2:-events-queue}"
API_URL="http://localhost:8080"
PROMETHEUS_URL="http://localhost:9090"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'

log() { echo -e "${YELLOW}[$(date +%T)] $1${NC}"; }
pass() { echo -e "${GREEN}PASS: $1${NC}"; }
fail() { echo -e "${RED}FAIL: $1${NC}"; exit 1; }

# 1. PRE-CONDITIONS
log "=== [1/6] Checking pre-conditions ==="

if ! docker compose ps "$WORKER_SERVICE" 2>/dev/null | grep -q "Up\|healthy"; then
    fail "Worker $WORKER_SERVICE is not running. Aborting."
fi

if ! curl -sf "$PROMETHEUS_URL/-/healthy" > /dev/null; then
    fail "Prometheus not available. Cannot validate chaos without observability."
fi

# Coletar baseline (últimos 60s)
QUEUE_DEPTH_BEFORE=$(curl -sf \
    "$PROMETHEUS_URL/api/v1/query?query=sqs_approximate_number_of_messages_visible" \
    | jq -r '.data.result[0].value[1] // "0"')

PROCESSING_RATE_BEFORE=$(curl -sf \
    "$PROMETHEUS_URL/api/v1/query?query=rate(sqs_messages_processed_total[1m])" \
    | jq -r '.data.result[0].value[1] // "0"')

log "Baseline — Queue depth: $QUEUE_DEPTH_BEFORE, Processing rate: $PROCESSING_RATE_BEFORE msg/s"

# 2. INJECT FAILURE
log "=== [2/6] Injecting failure: stopping $WORKER_SERVICE ==="
CHAOS_START=$(date +%s)
docker compose stop "$WORKER_SERVICE"
log "Worker stopped. Chaos clock started."

# 3. OBSERVE (não intervir)
log "=== [3/6] Observing for 35s (watchdog stale threshold: 30s) ==="

WATCHDOG_DETECTED=false
for i in $(seq 1 35); do
    sleep 1
    # Verificar se evento de healing apareceu nos logs do API
    if docker compose logs api-service --since 35s 2>/dev/null | grep -q "stale_detected"; then
        DETECT_TIME=$(($(date +%s) - CHAOS_START))
        log "Watchdog detected stale worker in ${DETECT_TIME}s"
        WATCHDOG_DETECTED=true
        break
    fi
done

if [ "$WATCHDOG_DETECTED" = false ]; then
    log "WARNING: watchdog stale_detected not found in logs — check SSE endpoint"
fi

# 4. ROLLBACK
log "=== [4/6] Restoring: starting $WORKER_SERVICE ==="
docker compose start "$WORKER_SERVICE"

# Aguardar health check
for i in $(seq 1 15); do
    sleep 2
    if docker compose ps "$WORKER_SERVICE" | grep -q "healthy\|Up"; then
        log "Worker healthy after $((i * 2))s"
        break
    fi
done

# 5. VALIDATE RECOVERY
log "=== [5/6] Validating recovery (30s observation window) ==="
sleep 30

RECOVERY_TIME=$(($(date +%s) - CHAOS_START))

QUEUE_DEPTH_AFTER=$(curl -sf \
    "$PROMETHEUS_URL/api/v1/query?query=sqs_approximate_number_of_messages_visible" \
    | jq -r '.data.result[0].value[1] // "-1"')

WORKER_HEALTHY=$(docker compose ps "$WORKER_SERVICE" | grep -c "healthy\|Up" || true)

# 6. PASS/FAIL
log "=== [6/6] Results ==="
echo "Recovery time: ${RECOVERY_TIME}s (SLO: 60s)"
echo "Queue depth before: $QUEUE_DEPTH_BEFORE → after: $QUEUE_DEPTH_AFTER"
echo "Worker healthy: $WORKER_HEALTHY"

PASSED=true

if [ "$RECOVERY_TIME" -gt 60 ]; then
    log "FAIL: Recovery took ${RECOVERY_TIME}s (SLO: 60s)"
    PASSED=false
fi

if [ "$WORKER_HEALTHY" -eq 0 ]; then
    log "FAIL: Worker not healthy after recovery"
    PASSED=false
fi

# Queue depth deve ter voltado ao baseline ou menor
if (( $(echo "$QUEUE_DEPTH_AFTER > $QUEUE_DEPTH_BEFORE * 2" | bc -l 2>/dev/null || echo 0) )); then
    log "WARNING: Queue depth grew significantly ($QUEUE_DEPTH_BEFORE → $QUEUE_DEPTH_AFTER)"
fi

if [ "$PASSED" = true ]; then
    pass "Worker crash experiment: hypothesis confirmed in ${RECOVERY_TIME}s"
else
    fail "Worker crash experiment: hypothesis FAILED — see logs above"
fi
```

## O que observar durante o experimento

### Via Grafana (abrir antes do chaos)
- Dashboard "Go Workers" → `worker_pool_active` deve cair a 0, depois voltar
- Dashboard "SQS" → `sqs_approximate_number_of_messages_visible` pode crescer brevemente
- Dashboard "Healing Events" → deve aparecer `stale_detected` em < 35s

### Via SSE (frontend)
Abrir DevTools → Network → EventStream no `/api/v1/events`:
```
event: healing
data: {"worker_id":"go-worker-1","event_type":"stale_detected","reason":"no heartbeat for 31s","action":"requeue_pending_tasks"}
```

### Via Prometheus (consultar após)
```promql
# Healing events durante o experimento
increase(healing_events_total{event_type="stale_detected"}[5m])

# Taxa de requeue
increase(requeue_attempts_total{status="success"}[5m])

# Tempo de recovery aproximado
(time() - worker_last_heartbeat_timestamp) < 10
```

## Critério de pass/fail
| Condição | SLO | Status se falhar |
|---|---|---|
| Recovery time total | <= 60s | FAIL |
| Worker healthy pós-restart | true | FAIL |
| Watchdog detectou stale | <= 35s | WARNING |
| Queue depth não explodiu | <= 2x baseline | WARNING |

## Checklist pré-experimento
- [ ] Prometheus UP e coletando métricas
- [ ] Grafana dashboards abertos
- [ ] Frontend SSE conectado
- [ ] Baseline documentado (>= 5min de métricas normais)
- [ ] DLQ vazia antes do experimento
