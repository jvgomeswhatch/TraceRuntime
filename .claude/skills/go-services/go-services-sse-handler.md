---
name: go-services:sse-handler
description: Handler SSE em Go com channel bounded, heartbeat periódico, cleanup de clientes desconectados e suporte a múltiplos event types. Compatível com o hook useSSE do frontend Next.js deste projeto.
---

# Skill: go-services:sse-handler

## Input necessário
1. Quais event types serão transmitidos? (ex: `worker_status`, `queue_lag`, `healing`)
2. Máximo de clientes simultâneos esperados (default: 50)
3. Intervalo de heartbeat (default: 15s)

## O que gerar

### `internal/sse/broker.go`
```go
package sse

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "sync"
    "time"
)

const (
    clientBufferSize  = 16    // bounded — cliente lento não bloqueia broker
    maxClients        = 50    // hard limit — rejeitar além disso
    heartbeatInterval = 15 * time.Second
)

// Event é o envelope de todo evento SSE deste projeto.
type Event struct {
    Type    string          `json:"type"`    // "worker_status" | "queue_lag" | "healing" | "heartbeat"
    Payload json.RawMessage `json:"payload"` // dado específico do event type
    TraceID string          `json:"trace_id,omitempty"`
}

type client struct {
    ch chan Event
    id string
}

// Broker gerencia clientes SSE conectados e faz broadcast de eventos.
// Thread-safe. Projetado para <50 clientes locais.
type Broker struct {
    mu      sync.RWMutex
    clients map[string]*client
    logger  *slog.Logger
}

func NewBroker(logger *slog.Logger) *Broker {
    return &Broker{
        clients: make(map[string]*client),
        logger:  logger,
    }
}

// Broadcast envia evento para todos os clientes conectados.
// Clientes com buffer cheio são dropados (não bloqueia).
func (b *Broker) Broadcast(event Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    for id, c := range b.clients {
        select {
        case c.ch <- event:
        default:
            // Cliente com buffer cheio — não bloquear; será detectado no próximo heartbeat
            b.logger.Warn("sse.client_buffer_full", "client_id", id)
        }
    }
}

// ServeHTTP implementa http.Handler — montar em /api/v1/events
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    b.mu.Lock()
    if len(b.clients) >= maxClients {
        b.mu.Unlock()
        http.Error(w, "too many SSE clients", http.StatusServiceUnavailable)
        return
    }

    clientID := fmt.Sprintf("%s-%d", r.RemoteAddr, time.Now().UnixNano())
    c := &client{
        ch: make(chan Event, clientBufferSize),
        id: clientID,
    }
    b.clients[clientID] = c
    b.mu.Unlock()

    defer func() {
        b.mu.Lock()
        delete(b.clients, clientID)
        close(c.ch)
        b.mu.Unlock()
        b.logger.Info("sse.client_disconnected", "client_id", clientID,
            "total_clients", b.ClientCount())
    }()

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no") // desabilitar nginx buffering
    w.WriteHeader(http.StatusOK)

    b.logger.Info("sse.client_connected", "client_id", clientID,
        "total_clients", b.ClientCount())

    flusher, ok := w.(http.Flusher)
    if !ok {
        b.logger.Error("sse.flusher_not_supported")
        return
    }

    heartbeat := time.NewTicker(heartbeatInterval)
    defer heartbeat.Stop()

    for {
        select {
        case <-r.Context().Done():
            return

        case <-heartbeat.C:
            fmt.Fprintf(w, "event: heartbeat\ndata: {\"ts\":%d}\n\n", time.Now().Unix())
            flusher.Flush()

        case event, ok := <-c.ch:
            if !ok {
                return
            }
            data, err := json.Marshal(event)
            if err != nil {
                b.logger.Error("sse.marshal_error", "error", err)
                continue
            }
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
            flusher.Flush()
        }
    }
}

func (b *Broker) ClientCount() int {
    b.mu.RLock()
    defer b.mu.RUnlock()
    return len(b.clients)
}
```

### Registrar no `main.go` / router
```go
broker := sse.NewBroker(logger)

mux := http.NewServeMux()
mux.Handle("/api/v1/events", broker)    // SSE endpoint
mux.HandleFunc("/health", healthHandler)
mux.Handle("/metrics", metrics.Handler())

// Passar broker para handlers e watchdog que precisam emitir eventos
watchdog := watchdog.New(db, broker, requeuer, logger) // broker implementa SSEBroadcaster
go watchdog.Run(ctx)
```

### Emitir evento de worker status (exemplo de uso)
```go
func emitWorkerStatus(broker *sse.Broker, workerID, status string) {
    payload, _ := json.Marshal(map[string]string{
        "worker_id": workerID,
        "status":    status,
    })
    broker.Broadcast(sse.Event{
        Type:    "worker_status",
        Payload: payload,
    })
}
```

## Checklist pós-geração
- [ ] `clientBufferSize = 16` bounded — não usar channel unbuffered
- [ ] `maxClients = 50` — rejeitar além com 503
- [ ] Heartbeat a cada 15s — frontend detecta conexão morta
- [ ] `X-Accel-Buffering: no` no header — nginx não bufferiza SSE
- [ ] `defer delete(b.clients, clientID)` no handler — cleanup garantido
- [ ] `Broker` implementa interface `SSEBroadcaster` do watchdog
- [ ] Porta 8080 exposta no docker-compose para Next.js conectar
- [ ] Testar: `curl -N http://localhost:8080/api/v1/events`
