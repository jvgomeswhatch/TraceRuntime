---
name: protobuf:new-event-schema
description: Scaffold de arquivo .proto para novo evento do sistema EventDrive — envelope padrão + payload específico, convenção de nomes, package correto, buf lint clean. Gera stubs Go e Python automaticamente.
---

# Skill: protobuf:new-event-schema

## Input necessário
1. Nome do domínio (ex: `order`, `payment`, `inference`)
2. Nome do evento (ex: `created`, `confirmed`, `failed`)
3. Campos do payload específico (nome, tipo proto3)

## O que gerar

### 1. `proto/eventdrive/v1/<dominio>.proto` (exemplo: order.proto)
```protobuf
syntax = "proto3";
package eventdrive.v1;

option go_package = "github.com/eventdrive/proto/gen/go/eventdrive/v1;eventdrivev1";
option java_package = "io.eventdrive.v1"; // manter para futura compatibilidade

import "google/protobuf/timestamp.proto";
import "eventdrive/v1/events.proto";

// OrderCreatedEvent é publicado quando um novo order é aceito pelo sistema.
// Campos existentes NUNCA são removidos ou renumerados.
// Novos campos: usar field numbers >= 20.
message OrderCreatedEvent {
  EventEnvelope envelope = 1;  // sempre campo 1

  // Payload específico de Order — campos 2..19
  string order_id   = 2;
  string user_id    = 3;
  int64  amount_brl = 4;  // centavos — sem float para dinheiro
  string status     = 5;  // "pending" | "processing"

  // Reservar campo 6..19 para evolução futura

  // Campos adicionados em v1.1 e posteriores: >= 20
  // string coupon_code = 20;
}

// OrderFailedEvent é publicado quando processamento falha após retries.
message OrderFailedEvent {
  EventEnvelope envelope = 1;

  string order_id     = 2;
  string failure_code = 3;  // "payment_declined" | "inventory_unavailable" | "timeout"
  string reason       = 4;
}
```

### 2. `proto/eventdrive/v1/events.proto` (envelope base — se não existir)
```protobuf
syntax = "proto3";
package eventdrive.v1;

option go_package = "github.com/eventdrive/proto/gen/go/eventdrive/v1;eventdrivev1";

import "google/protobuf/timestamp.proto";

// EventEnvelope é o envelope obrigatório de todos os eventos do sistema.
// NUNCA adicionar campos sem incrementar a documentação de compatibilidade.
message EventEnvelope {
  string trace_id        = 1;  // W3C trace-id — propagado de ponta a ponta
  string event_type      = 2;  // ex: "order.created" — snake_case do package.message
  string event_id        = 3;  // UUID v4 — chave de idempotência
  google.protobuf.Timestamp timestamp = 4;
  string source_service  = 5;  // ex: "order-service"
  int32  schema_version  = 6;  // incrementar em breaking changes (deve ser raro)
  // Novos campos do envelope: >= 10
}
```

### 3. `proto/buf.yaml` (se não existir)
```yaml
version: v1
name: buf.build/eventdrive/schemas
deps:
  - buf.build/googleapis/googleapis   # para google/protobuf/timestamp.proto
lint:
  use:
    - DEFAULT
breaking:
  use:
    - FILE
```

### 4. `proto/buf.gen.yaml` (se não existir)
```yaml
version: v1
managed:
  enabled: true
  go_package_prefix:
    default: github.com/eventdrive/proto/gen/go
plugins:
  - plugin: go
    out: gen/go
    opt: paths=source_relative
  - plugin: go-grpc
    out: gen/go
    opt: paths=source_relative
  - plugin: python
    out: gen/python
  - plugin: grpc_python
    out: gen/python
```

## Passos de execução após criar o .proto

```bash
cd proto/

# 1. Lint — deve passar sem warnings
buf lint

# 2. Breaking change check contra main
buf breaking --against '.git#branch=main'

# 3. Gerar stubs Go e Python
buf generate

# 4. Verificar stubs gerados
ls gen/go/eventdrive/v1/
ls gen/python/eventdrive/v1/
```

## Checklist pós-geração
- [ ] `package eventdrive.v1` e `go_package` corretos
- [ ] `EventEnvelope envelope = 1` é o campo 1 de todo evento
- [ ] `buf lint` passa sem warnings
- [ ] `buf breaking` passa (sem campos removidos ou renumerados)
- [ ] Stubs gerados em `gen/go/` e `gen/python/`
- [ ] Import nos serviços Go atualizado com o novo tipo
- [ ] Import nos serviços Python atualizado
