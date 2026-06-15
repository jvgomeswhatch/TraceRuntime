---
name: autohealing:worker-watchdog
description: Goroutine watchdog em Go que consulta worker_heartbeats, detecta workers stale, emite eventos SSE de healing e dispara requeue de tarefas pendentes. Inclui circuit breaker para evitar restart storm.
---

# Skill: autohealing:worker-watchdog

## Input necessário
1. Stale threshold desejado (default: 30s)
2. O canal SSE já existe no API service? Se não, criar junto com esta skill
3. Máximo de restarts antes de throttle (default: 3 em 5min)

## O que gerar

### `internal/watchdog/watchdog.go`
```go
package watchdog

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

const (
    staleThreshold = 30 * time.Second
    watchInterval  = 10 * time.Second
    maxRestarts    = 3
    restartWindow  = 5 * time.Minute
)

// HealingEvent publicado no canal SSE e logado.
type HealingEvent struct {
    WorkerID  string    `json:"worker_id"`
    EventType string    `json:"event_type"` // stale_detected | throttled | requeue_triggered | recovered
    Reason    string    `json:"reason"`
    Action    string    `json:"action"`
    TraceID   string    `json:"trace_id"`
    Timestamp time.Time `json:"timestamp"`
}

type SSEBroadcaster interface {
    Broadcast(event HealingEvent)
}

type Requeuer interface {
    RequeueStale(ctx context.Context, workerID string) (int, error)
}

type Watchdog struct {
    db          *pgxpool.Pool
    broadcaster SSEBroadcaster
    requeuer    Requeuer
    logger      *slog.Logger

    mu           sync.Mutex
    restartLog   map[string][]time.Time // workerID → timestamps de restart
}

func New(db *pgxpool.Pool, broadcaster SSEBroadcaster, requeuer Requeuer, logger *slog.Logger) *Watchdog {
    return &Watchdog{
        db:          db,
        broadcaster: broadcaster,
        requeuer:    requeuer,
        logger:      logger,
        restartLog:  make(map[string][]time.Time),
    }
}

func (w *Watchdog) Run(ctx context.Context) {
    ticker := time.NewTicker(watchInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.check(ctx)
        }
    }
}

func (w *Watchdog) check(ctx context.Context) {
    rows, err := w.db.Query(ctx, `
        SELECT worker_id, worker_type, last_seen, restart_count
        FROM worker_heartbeats
        WHERE status = 'alive'
          AND last_seen < NOW() - $1::interval
    `, staleThreshold.String())
    if err != nil {
        w.logger.Error("watchdog.query_failed", "error", err)
        return
    }
    defer rows.Close()

    for rows.Next() {
        var workerID, workerType string
        var lastSeen time.Time
        var restartCount int

        if err := rows.Scan(&workerID, &workerType, &lastSeen, &restartCount); err != nil {
            continue
        }

        staleDuration := time.Since(lastSeen).Round(time.Second)

        if w.isThrottled(workerID) {
            w.emit(HealingEvent{
                WorkerID:  workerID,
                EventType: "throttled",
                Reason:    fmt.Sprintf("max restarts (%d) in window", maxRestarts),
                Action:    "manual_intervention_required",
                Timestamp: time.Now(),
            })
            continue
        }

        w.logger.Warn("watchdog.stale_detected",
            "worker_id", workerID,
            "stale_for", staleDuration)

        // Emitir antes de agir — frontend vê diagnóstico
        w.emit(HealingEvent{
            WorkerID:  workerID,
            EventType: "stale_detected",
            Reason:    fmt.Sprintf("no heartbeat for %s", staleDuration),
            Action:    "requeue_pending_tasks",
            Timestamp: time.Now(),
        })

        n, err := w.requeuer.RequeueStale(ctx, workerID)
        if err != nil {
            w.logger.Error("watchdog.requeue_failed",
                "worker_id", workerID, "error", err)
        } else {
            w.logger.Info("watchdog.requeued",
                "worker_id", workerID, "count", n)
            w.recordRestart(workerID)
        }

        // Marcar como stale no DB
        _, _ = w.db.Exec(ctx,
            "UPDATE worker_heartbeats SET status='stale' WHERE worker_id=$1",
            workerID)
    }
}

func (w *Watchdog) emit(event HealingEvent) {
    w.logger.Info("healing_event",
        "worker_id", event.WorkerID,
        "event_type", event.EventType,
        "reason", event.Reason,
        "action", event.Action)

    w.broadcaster.Broadcast(event)
}

func (w *Watchdog) isThrottled(workerID string) bool {
    w.mu.Lock()
    defer w.mu.Unlock()

    now := time.Now()
    cutoff := now.Add(-restartWindow)

    // Limpar entradas antigas
    var recent []time.Time
    for _, t := range w.restartLog[workerID] {
        if t.After(cutoff) {
            recent = append(recent, t)
        }
    }
    w.restartLog[workerID] = recent

    return len(recent) >= maxRestarts
}

func (w *Watchdog) recordRestart(workerID string) {
    w.mu.Lock()
    defer w.mu.Unlock()
    w.restartLog[workerID] = append(w.restartLog[workerID], time.Now())
}
```

### Integrar no API service (`cmd/server/main.go`)
```go
// Após setup do db e SSE broadcaster:
watchdog := watchdog.New(db, sseBroadcaster, sqsRequeuer, logger)
go watchdog.Run(ctx)
logger.Info("watchdog.started")
```

## Checklist pós-geração
- [ ] `SSEBroadcaster` interface implementada pelo handler SSE existente
- [ ] `Requeuer` interface implementada pelo SQS client
- [ ] Watchdog rodando em goroutine separada com o mesmo `ctx` do graceful shutdown
- [ ] `isThrottled` previne restart storm (circuit breaker)
- [ ] Evento SSE emitido ANTES da ação de healing — frontend sempre vê diagnóstico
- [ ] Index `idx_worker_heartbeats_last_seen` presente — query é eficiente
- [ ] Métrica Prometheus `healing_events_total{event_type}` incrementada no `emit`
