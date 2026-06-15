---
name: autohealing:heartbeat-tracker
description: Implementa heartbeat tracking no worker Go — goroutine de upsert periódico na tabela worker_heartbeats do PostgreSQL, com worker_id único por instância, shutdown gracioso e log estruturado.
---

# Skill: autohealing:heartbeat-tracker

## Input necessário
1. Nome do serviço worker (ex: `go-worker`)
2. Intervalo de heartbeat desejado (default: 10s)
3. A tabela `worker_heartbeats` já existe? Se não, incluir migration

## O que gerar

### 1. Migration SQL (`db/migrations/002_worker_heartbeats.sql`)
```sql
-- Executar apenas se tabela não existir
CREATE TABLE IF NOT EXISTS worker_heartbeats (
    worker_id       TEXT PRIMARY KEY,
    worker_type     TEXT NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'alive',
    restart_count   INT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_worker_heartbeats_last_seen
    ON worker_heartbeats(last_seen);
```

### 2. `internal/heartbeat/tracker.go`
```go
package heartbeat

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

const (
    defaultInterval = 10 * time.Second
    workerType      = "go-worker"
)

type Tracker struct {
    db       *pgxpool.Pool
    workerID string
    interval time.Duration
    logger   *slog.Logger
}

func New(db *pgxpool.Pool, logger *slog.Logger) *Tracker {
    workerID := os.Getenv("WORKER_ID")
    if workerID == "" {
        hostname, _ := os.Hostname()
        workerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
    }

    return &Tracker{
        db:       db,
        workerID: workerID,
        interval: defaultInterval,
        logger:   logger.With("worker_id", workerID),
    }
}

// Run inicia o loop de heartbeat. Bloqueia até ctx ser cancelado.
// Chamar em goroutine separada no main.go.
func (t *Tracker) Run(ctx context.Context) {
    // Registrar na inicialização
    if err := t.upsert(ctx, "alive"); err != nil {
        t.logger.Error("heartbeat.init_failed", "error", err)
    }

    ticker := time.NewTicker(t.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            // Marcar como offline antes de sair
            shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
            defer cancel()
            _ = t.upsert(shutdownCtx, "offline")
            t.logger.Info("heartbeat.stopped")
            return

        case <-ticker.C:
            if err := t.upsert(ctx, "alive"); err != nil {
                t.logger.Error("heartbeat.upsert_failed", "error", err)
                // Não fatal — continuar tentando
            }
        }
    }
}

func (t *Tracker) WorkerID() string { return t.workerID }

func (t *Tracker) upsert(ctx context.Context, status string) error {
    _, err := t.db.Exec(ctx, `
        INSERT INTO worker_heartbeats (worker_id, worker_type, last_seen, status, updated_at)
        VALUES ($1, $2, NOW(), $3, NOW())
        ON CONFLICT (worker_id) DO UPDATE SET
            last_seen  = NOW(),
            status     = $3,
            updated_at = NOW()
    `, t.workerID, workerType, status)

    if err != nil {
        return fmt.Errorf("heartbeat upsert: %w", err)
    }

    t.logger.Debug("heartbeat.ok", "status", status)
    return nil
}
```

### 3. Integrar no `cmd/worker/main.go`
```go
import "yourmodule/internal/heartbeat"

func main() {
    // ... setup db, logger, context ...

    tracker := heartbeat.New(db, logger)

    // Heartbeat em goroutine própria
    go tracker.Run(ctx)

    logger.Info("worker.started",
        "worker_id", tracker.WorkerID())

    // ... resto do worker (consumer loop, etc.) ...
}
```

### 4. Env vars necessárias
```yaml
# docker-compose.yml — no serviço go-worker
environment:
  WORKER_ID: "go-worker-1"   # ou omitir para usar hostname+pid
  DATABASE_URL: "postgres://user:pass@postgres:5432/eventdrive"
```

## Checklist pós-geração
- [ ] Migration executada antes de subir o serviço
- [ ] `tracker.Run(ctx)` em goroutine separada — não bloquear main loop
- [ ] `WORKER_ID` único por instância (se escalar para múltiplos workers)
- [ ] Watchdog configurado com mesmo stale threshold > heartbeat interval (ex: 30s stale para 10s interval)
- [ ] `status = 'offline'` registrado no shutdown — watchdog não dispara falso positivo
- [ ] Index `idx_worker_heartbeats_last_seen` presente para query do watchdog ser eficiente
