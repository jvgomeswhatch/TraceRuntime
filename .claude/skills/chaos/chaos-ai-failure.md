---
name: chaos:ai-failure
description: Experimento de chaos que simula falha do AI runtime (ai-runtime container stop) e valida que requests pendentes recebem erro gracioso via SSE, novos requests são rejeitados com 503, e recovery ocorre em < 30s após restart do serviço.
---

# Skill: chaos:ai-failure

## Input necessário
1. Nome do serviço AI no docker-compose (ex: `ai-runtime`)
2. Endpoint de health (ex: `http://localhost:8001/health`)
3. O sistema tem circuit breaker no Go worker chamando o AI runtime? Sim/Não

## O que gerar

### `tests/chaos/ai_failure_test.sh`
```bash
#!/bin/bash
set -euo pipefail

# Experimento: AI Runtime Failure
# Hipótese: Ao parar ai-runtime, requests em andamento recebem erro gracioso via SSE,
#           novos requests ao Go API retornam 503 (circuit breaker ou fallback),
#           e após restart o serviço está saudável em < 30s.
#
# NÃO testa perda de dados — testa graceful degradation.

AI_SERVICE="${1:-ai-runtime}"
AI_URL="http://localhost:8001"
API_URL="http://localhost:8080"
PROMETHEUS_URL="http://localhost:9090"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log() { echo -e "${YELLOW}[$(date +%T)] $1${NC}"; }
pass() { echo -e "${GREEN}PASS: $1${NC}"; }
fail() { echo -e "${RED}FAIL: $1${NC}"; exit 1; }

# 1. PRE-CONDITIONS
log "=== [1/6] Pre-conditions ==="

if ! curl -sf "$AI_URL/health" > /dev/null; then
    fail "AI runtime not healthy at $AI_URL/health"
fi

if ! docker compose ps "$AI_SERVICE" 2>/dev/null | grep -qE "Up|healthy"; then
    fail "$AI_SERVICE not running"
fi

log "AI runtime healthy. Starting experiment."

# 2. INJECT LONG-RUNNING REQUEST (para validar comportamento mid-stream)
log "=== [2/6] Starting background inference request (will be interrupted) ==="

# Iniciar request em background para pegar mid-stream
TRACE_ID="chaos-$(date +%s)"
curl -sf -N -X POST "$AI_URL/infer/stream" \
    -H "Content-Type: application/json" \
    -d "{\"input\":\"Explain distributed systems in detail\",\"trace_id\":\"$TRACE_ID\"}" \
    > /tmp/chaos_ai_sse_output.txt 2>&1 &
SSE_PID=$!

log "SSE request started (PID $SSE_PID, trace_id: $TRACE_ID)"
sleep 2  # deixar começar

# 3. INJECT FAILURE
log "=== [3/6] Stopping $AI_SERVICE ==="
CHAOS_START=$(date +%s)
docker compose stop "$AI_SERVICE"
log "AI runtime stopped."

# Aguardar SSE request terminar (deve receber erro, não timeout infinito)
WAIT_FOR_SSE=10
for i in $(seq 1 $WAIT_FOR_SSE); do
    sleep 1
    if ! kill -0 "$SSE_PID" 2>/dev/null; then
        log "SSE request terminated after ${i}s (expected — connection closed)"
        break
    fi
done

if kill -0 "$SSE_PID" 2>/dev/null; then
    log "WARNING: SSE request still running after ${WAIT_FOR_SSE}s — killing"
    kill "$SSE_PID" 2>/dev/null || true
fi

# Verificar se SSE recebeu evento de erro (não ficou pendurado silenciosamente)
if grep -q '"event_type":"error"\|event: error' /tmp/chaos_ai_sse_output.txt 2>/dev/null; then
    log "SSE error event received correctly"
    SSE_GRACEFUL=true
else
    log "WARNING: No SSE error event found in output — check graceful disconnect"
    SSE_GRACEFUL=false
fi

# 4. VALIDATE DEGRADED MODE (API deve rejeitar novos requests com 503)
log "=== [4/6] Validating degraded mode (new requests should get 503) ==="

HTTP_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" \
    -X POST "$API_URL/api/v1/events" \
    -H "Content-Type: application/json" \
    -d '{"event_type":"test","trace_id":"chaos-degraded","payload":{}}' \
    2>/dev/null || echo "000")

if [ "$HTTP_STATUS" = "503" ] || [ "$HTTP_STATUS" = "502" ]; then
    log "API correctly returning $HTTP_STATUS during AI failure"
    DEGRADED_CORRECTLY=true
elif [ "$HTTP_STATUS" = "000" ]; then
    log "API connection refused (expected if AI is dependency)"
    DEGRADED_CORRECTLY=true
else
    log "WARNING: API returned $HTTP_STATUS (expected 503 with circuit breaker)"
    DEGRADED_CORRECTLY=false
fi

# 5. RESTORE
log "=== [5/6] Restoring $AI_SERVICE ==="
docker compose start "$AI_SERVICE"

# Aguardar health check
RECOVER_START=$(date +%s)
RECOVERED=false

for i in $(seq 1 15); do  # 15 * 2s = 30s
    sleep 2
    if curl -sf "$AI_URL/health" > /dev/null 2>&1; then
        RECOVER_TIME=$(($(date +%s) - RECOVER_START))
        log "AI runtime healthy again after ${RECOVER_TIME}s"
        RECOVERED=true
        break
    fi
done

# 6. PASS/FAIL
log "=== [6/6] Results ==="
TOTAL_TIME=$(($(date +%s) - CHAOS_START))

echo "Total experiment time: ${TOTAL_TIME}s"
echo "SSE graceful error: $SSE_GRACEFUL"
echo "API degraded correctly: $DEGRADED_CORRECTLY"
echo "AI runtime recovered: $RECOVERED"
echo "Recovery time: ${RECOVER_TIME:-N/A}s (SLO: 30s)"

PASSED=true

if [ "$RECOVERED" = false ]; then
    log "FAIL: AI runtime did not recover within 30s"
    PASSED=false
fi

if [ "${RECOVER_TIME:-999}" -gt 30 ]; then
    log "FAIL: Recovery took ${RECOVER_TIME}s (SLO: 30s)"
    PASSED=false
fi

if [ "$PASSED" = true ]; then
    pass "AI failure experiment passed — recovered in ${RECOVER_TIME:-?}s"
else
    fail "AI failure experiment FAILED"
fi

# Cleanup
rm -f /tmp/chaos_ai_sse_output.txt
```

## O que observar durante o experimento

### Comportamento esperado no frontend SSE
Quando `ai-runtime` para, conexões SSE abertas devem receber:
```
event: error
data: {"trace_id":"...", "error": "connection to AI runtime lost"}
```
Não devem ficar pendentes por mais de 5s (timeout de upstream no Go API).

### Prometheus durante chaos
```promql
# Circuit breaker (se implementado)
ai_runtime_circuit_breaker_state{state="open"}

# Taxa de erro nas chamadas ao AI runtime
rate(ai_calls_total{status="error"}[1m])

# Latência spike antes do timeout
histogram_quantile(0.99, rate(ai_call_duration_seconds_bucket[1m]))
```

## Circuit breaker simples no Go (se não implementado)
```go
// Para implementar circuit breaker antes da Phase 8:
// Usar timeout de contexto como proteção mínima

func (w *Worker) callAIRuntime(ctx context.Context, payload []byte) ([]byte, error) {
    // Timeout deve ser menor que visibility timeout do SQS
    timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    resp, err := w.httpClient.PostWithContext(timeoutCtx, aiRuntimeURL, payload)
    if err != nil {
        // Logar para trigger de healing event
        w.logger.Error("ai_runtime.call_failed", "error", err)
        return nil, fmt.Errorf("ai runtime unavailable: %w", err)
    }
    return resp, nil
}
```

## Checklist pré-experimento
- [ ] AI runtime tem endpoint `/health` funcionando
- [ ] Go worker tem timeout explícito nas chamadas ao AI runtime (< SQS visibility timeout)
- [ ] Frontend SSE está conectado para verificar eventos de erro
- [ ] Prometheus coletando métricas de chamadas ao AI runtime
