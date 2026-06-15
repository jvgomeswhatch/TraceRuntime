---
name: testing:otel-trace-validator
description: Valida que trace_id propagou por todo o fluxo end-to-end — HTTP → Go API → SQS → Go Worker → AI Runtime → Frontend SSE. Consulta Grafana Tempo via API HTTP e verifica que todos os spans esperados estão presentes no mesmo trace.
---

# Skill: testing:otel-trace-validator

## Input necessário
1. trace_id a validar (gerado pelo teste de integração ou capturado do frontend)
2. Quais serviços devem aparecer no trace? (ex: `order-service`, `go-worker`, `ai-runtime`)
3. URL do Tempo (default: `http://localhost:3200`)

## O que gerar

### `tests/integration/trace_validator_test.go`
```go
package integration

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "testing"
    "time"
)

// TempoTrace representa a resposta da API do Grafana Tempo
type TempoTrace struct {
    Batches []struct {
        Resource struct {
            Attributes []struct {
                Key   string `json:"key"`
                Value struct {
                    StringValue string `json:"stringValue"`
                } `json:"value"`
            } `json:"attributes"`
        } `json:"resource"`
        ScopeSpans []struct {
            Spans []struct {
                Name       string `json:"name"`
                TraceID    string `json:"traceId"`
                SpanID     string `json:"spanId"`
                ParentSpanID string `json:"parentSpanId"`
                Status     struct {
                    Code    string `json:"code"`
                    Message string `json:"message"`
                } `json:"status"`
            } `json:"spans"`
        } `json:"scopeSpans"`
    } `json:"batches"`
}

const (
    tempoURL       = "http://localhost:3200"
    traceWaitTimeout = 30 * time.Second // traces levam alguns segundos para aparecer no Tempo
)

// ValidateEndToEndTrace verifica que um trace_id tem spans de todos os serviços esperados.
// Usar após um request de integração para confirmar propagação completa.
func ValidateEndToEndTrace(t *testing.T, traceID string, expectedServices []string) {
    t.Helper()

    // Aguardar trace aparecer no Tempo (ingestion tem delay)
    var trace *TempoTrace
    deadline := time.Now().Add(traceWaitTimeout)

    for time.Now().Before(deadline) {
        var err error
        trace, err = fetchTrace(traceID)
        if err == nil && trace != nil && len(trace.Batches) > 0 {
            break
        }
        time.Sleep(2 * time.Second)
    }

    if trace == nil || len(trace.Batches) == 0 {
        t.Fatalf("trace %s not found in Tempo after %s", traceID, traceWaitTimeout)
    }

    // Extrair serviços presentes no trace
    foundServices := map[string]bool{}
    for _, batch := range trace.Batches {
        for _, attr := range batch.Resource.Attributes {
            if attr.Key == "service.name" {
                foundServices[attr.Value.StringValue] = true
            }
        }
    }

    // Verificar que todos os serviços esperados estão presentes
    for _, expected := range expectedServices {
        if !foundServices[expected] {
            t.Errorf("service %q missing from trace %s. Found: %v",
                expected, traceID, keys(foundServices))
        }
    }

    // Verificar ausência de spans com status ERROR
    for _, batch := range trace.Batches {
        for _, scope := range batch.ScopeSpans {
            for _, span := range scope.Spans {
                if span.Status.Code == "STATUS_CODE_ERROR" {
                    t.Errorf("span %q has error status: %s",
                        span.Name, span.Status.Message)
                }
            }
        }
    }

    t.Logf("trace %s validated: services=%v", traceID, keys(foundServices))
}

func fetchTrace(traceID string) (*TempoTrace, error) {
    url := fmt.Sprintf("%s/api/traces/%s", tempoURL, traceID)
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusNotFound {
        return nil, nil // ainda não indexado
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("tempo returned %d", resp.StatusCode)
    }

    var trace TempoTrace
    if err := json.NewDecoder(resp.Body).Decode(&trace); err != nil {
        return nil, err
    }
    return &trace, nil
}

func keys(m map[string]bool) []string {
    result := make([]string, 0, len(m))
    for k := range m {
        result = append(result, k)
    }
    return result
}
```

### `tests/integration/e2e_trace_test.go` — uso do validator
```go
package integration

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "testing"
    "time"
)

// TestEndToEndTracePropagate envia um evento pelo fluxo completo e valida
// que o trace_id propagou por todos os serviços.
func TestEndToEndTracePropagate(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e trace test in short mode")
    }

    // 1. Gerar trace_id único
    traceID := fmt.Sprintf("%032x", time.Now().UnixNano()) // hex 32 chars = W3C trace-id

    // 2. Enviar request com traceparent header
    traceparent := fmt.Sprintf("00-%s-0000000000000001-01", traceID)

    payload := map[string]interface{}{
        "event_type": "test.e2e",
        "trace_id":   traceID,
        "payload":    map[string]string{"test": "true"},
    }
    body, _ := json.Marshal(payload)

    req, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/events",
        strings.NewReader(string(body)))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("traceparent", traceparent) // propagar para Go API

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("request failed: %v", err)
    }
    resp.Body.Close()

    if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200/202, got %d", resp.StatusCode)
    }

    // 3. Aguardar processamento (worker + AI runtime)
    // Em ambiente local com AI runtime, inferência pode levar até 60s
    time.Sleep(10 * time.Second)

    // 4. Validar trace no Tempo
    // Ajustar expectedServices conforme fase atual do projeto
    expectedServices := []string{
        "order-service",  // Phase 1+
        // "go-worker",   // Phase 2+
        // "ai-runtime",  // Phase 3+
    }

    ValidateEndToEndTrace(t, traceID, expectedServices)
}
```

### Script bash para validação rápida sem Go
```bash
#!/bin/bash
# tests/integration/validate_trace.sh <trace_id>

TRACE_ID="${1:-}"
TEMPO_URL="http://localhost:3200"

if [ -z "$TRACE_ID" ]; then
    echo "Usage: $0 <trace_id>"
    exit 1
fi

echo "Fetching trace $TRACE_ID from Tempo..."

# Aguardar trace aparecer
for i in $(seq 1 15); do
    RESPONSE=$(curl -sf "$TEMPO_URL/api/traces/$TRACE_ID" 2>/dev/null)
    if [ -n "$RESPONSE" ] && echo "$RESPONSE" | jq -e '.batches | length > 0' > /dev/null 2>&1; then
        break
    fi
    echo "Waiting for trace... (attempt $i/15)"
    sleep 2
done

if [ -z "$RESPONSE" ]; then
    echo "FAIL: trace $TRACE_ID not found in Tempo"
    exit 1
fi

# Extrair serviços presentes
echo "Services in trace:"
echo "$RESPONSE" | jq -r '
  .batches[].resource.attributes[]
  | select(.key == "service.name")
  | .value.stringValue
' | sort -u

# Verificar erros
ERROR_SPANS=$(echo "$RESPONSE" | jq -r '
  .batches[].scopeSpans[].spans[]
  | select(.status.code == "STATUS_CODE_ERROR")
  | .name
' 2>/dev/null)

if [ -n "$ERROR_SPANS" ]; then
    echo "WARN: Error spans found:"
    echo "$ERROR_SPANS"
else
    echo "OK: No error spans"
fi
```

## Checklist pós-validação
- [ ] trace_id aparece no Tempo em < 30s após request
- [ ] Todos os serviços esperados presentes no mesmo trace (mesmo trace_id)
- [ ] Nenhum span com `status.code = STATUS_CODE_ERROR`
- [ ] parent/child relationship correta: consumer span é filho do producer span
- [ ] trace_id do SSE frontend coincide com trace_id no Tempo
