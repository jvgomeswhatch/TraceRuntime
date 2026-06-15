---
name: database:new-schema
description: Cria schema SQL completo para um microsserviço — tabela principal, outbox, indexes e migration file. Segue padrão de schema por serviço com outbox pattern para publicação confiável de eventos.
---

# Skill: database:new-schema

## Input necessário
1. Nome do serviço/domínio (ex: `orders`, `payments`, `inventory`)
2. Entidade principal e seus campos
3. Quais eventos esse serviço publica? (para outbox)
4. Status possíveis da entidade?

## O que gerar

### `services/{service}/migrations/001_init.sql`
```sql
-- Schema isolado por serviço
CREATE SCHEMA IF NOT EXISTS {domain};

-- Tabela principal
CREATE TABLE {domain}.{entities} (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- campos do domínio aqui
    status      TEXT NOT NULL CHECK (status IN ({status_values})),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes de acesso comum
CREATE INDEX idx_{entities}_status 
    ON {domain}.{entities}(status) 
    WHERE status NOT IN ('cancelled', 'completed');  -- partial index — ignora finalizados

-- Outbox para publicação confiável de eventos
CREATE TABLE {domain}.outbox (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL,
    trace_id    TEXT NOT NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at     TIMESTAMPTZ  -- NULL = pendente, preenchido = enviado
);

-- Index parcial — só lê pendentes
CREATE INDEX idx_{domain}_outbox_pending 
    ON {domain}.outbox(created_at) 
    WHERE sent_at IS NULL;

-- Trigger para updated_at automático
CREATE OR REPLACE FUNCTION {domain}.set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_{entities}_updated_at
    BEFORE UPDATE ON {domain}.{entities}
    FOR EACH ROW EXECUTE FUNCTION {domain}.set_updated_at();
```

## Exemplo concreto (orders)
```sql
CREATE SCHEMA IF NOT EXISTS orders;

CREATE TABLE orders.orders (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('pending','confirmed','cancelled')),
    total_cents BIGINT NOT NULL CHECK (total_cents > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_customer ON orders.orders(customer_id);
CREATE INDEX idx_orders_status ON orders.orders(status) WHERE status = 'pending';

CREATE TABLE orders.outbox (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL,
    trace_id    TEXT NOT NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at     TIMESTAMPTZ
);

CREATE INDEX idx_orders_outbox_pending ON orders.outbox(created_at) WHERE sent_at IS NULL;
```

## Relay job — publicar outbox para SQS (Go)
```go
// Rodar em goroutine no startup do serviço
func (r *OutboxRelay) Run(ctx context.Context) {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            r.publishPending(ctx)
        }
    }
}

func (r *OutboxRelay) publishPending(ctx context.Context) {
    rows, _ := r.db.Query(ctx,
        `SELECT id, event_type, trace_id, payload FROM orders.outbox
         WHERE sent_at IS NULL ORDER BY created_at LIMIT 10`)
    // ... publicar no SQS, marcar sent_at = now()
}
```

## Checklist pós-geração
- [ ] Schema isolado por serviço (não misturar tabelas de serviços diferentes)
- [ ] Outbox criada junto com tabela principal
- [ ] Indexes apenas nas colunas usadas em queries reais
- [ ] Sem `SELECT *` nas queries Go — colunas explícitas
- [ ] Connection pool máx 10 conexões por serviço
- [ ] Migration file numerada sequencialmente (001, 002...)
