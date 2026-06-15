---
name: protobuf:generate-stubs
description: Executa buf lint + buf breaking + buf generate e verifica que stubs Go e Python foram gerados corretamente. Inclui troubleshooting de erros comuns do buf e verificação de imports nos serviços.
---

# Skill: protobuf:generate-stubs

## Input necessário
1. Houve mudanças no schema? (para decidir se rodar breaking check)
2. Qual serviço(s) usa os novos stubs? (para atualizar imports)

## O que gerar / executar

### Sequência de comandos (rodar na ordem)
```bash
cd proto/

# 1. Verificar buf instalado
buf --version  # deve ser >= 1.28

# 2. Atualizar dependências buf (googleapis, etc.)
buf dep update

# 3. Lint — DEVE passar limpo antes de gerar
buf lint
# Se falhar: corrigir warnings antes de continuar

# 4. Breaking change check contra main
# Apenas se houve mudança em schema existente
buf breaking --against '.git#branch=main'
# PASS = seguro gerar
# FAIL = campo removido/renumerado — CORRIGIR antes de continuar

# 5. Gerar stubs Go e Python
buf generate

# 6. Verificar arquivos gerados
ls gen/go/eventdrive/v1/
# Esperado: *.pb.go, *_grpc.pb.go

ls gen/python/eventdrive/v1/
# Esperado: *_pb2.py, *_pb2_grpc.py

# 7. Verificar que Go compila com os novos stubs
cd ../services/order-service && go build ./...
cd ../services/payment-service && go build ./...
```

### Troubleshooting de erros comuns

**Erro: `buf: plugin go not found`**
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# Verificar que $GOPATH/bin está no PATH
```

**Erro: `no such file or directory: google/protobuf/timestamp.proto`**
```bash
# Atualizar dependências buf
buf dep update
# Se persistir, adicionar ao buf.yaml:
# deps:
#   - buf.build/googleapis/googleapis
```

**Erro: `FIELD_SAME_NUMBER_IN_PARENT` (breaking change)**
```bash
# Causa: campo removido e novo campo com mesmo número
# Solução: NUNCA reusar field numbers
# Adicionar campo com novo número mais alto
# Marcar campo antigo como "reserved" comentado
```

**Erro: stubs Python com imports quebrados (`from eventdrive.v1 import ...`)**
```python
# Adicionar ao topo do arquivo que importa stubs Python:
import sys
sys.path.insert(0, '/app/proto/gen/python')
# Ou configurar PYTHONPATH no docker-compose:
# PYTHONPATH: /app/proto/gen/python
```

### Atualizar imports nos serviços Go
```go
// go.mod — adicionar replace se proto é local
replace github.com/eventdrive/proto => ../../proto

// Importar no serviço:
import eventdrivev1 "github.com/eventdrive/proto/gen/go/eventdrive/v1"

// Usar:
envelope := &eventdrivev1.EventEnvelope{
    TraceId:      traceID,
    EventType:    "order.created",
    EventId:      uuid.New().String(),
    Timestamp:    timestamppb.Now(),
    SourceService: "order-service",
}
```

### Atualizar imports nos serviços Python
```python
# No serviço Python (ai-runtime):
from eventdrive.v1 import order_pb2
from eventdrive.v1 import events_pb2

# Usar:
envelope = events_pb2.EventEnvelope(
    trace_id=trace_id,
    event_type="inference.completed",
    event_id=str(uuid.uuid4()),
    timestamp=Timestamp(seconds=int(time.time())),
    source_service="ai-runtime",
)
```

## Checklist pós-geração
- [ ] `buf lint` passa sem warnings
- [ ] `buf breaking` passa (ou nenhum schema existente foi alterado)
- [ ] `gen/go/eventdrive/v1/*.pb.go` gerados
- [ ] `gen/python/eventdrive/v1/*_pb2.py` gerados
- [ ] `go build ./...` nos serviços Go passa
- [ ] `python -c "from eventdrive.v1 import order_pb2"` passa
- [ ] Stubs commitados junto com o `.proto` no mesmo commit
