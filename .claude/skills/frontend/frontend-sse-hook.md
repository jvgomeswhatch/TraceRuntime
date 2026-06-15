---
name: frontend:sse-hook
description: Hook TypeScript useSSE com reconnect exponential backoff, buffer circular de 100 eventos, detecção de heartbeat perdido e limpeza de recursos no unmount. Compatível com o SSE broker Go deste projeto.
---

# Skill: frontend:sse-hook

## Input necessário
1. URL do endpoint SSE (ex: `http://localhost:8080/api/v1/events`)
2. Buffer máximo de eventos (default: 100)
3. Backoff máximo de reconexão (default: 30s)

## O que gerar

### `hooks/useSSE.ts`
```typescript
import { useCallback, useEffect, useRef, useState } from "react";

export interface SSEEvent {
  type: string;
  payload: Record<string, unknown>;
  receivedAt: number; // timestamp em ms
}

interface SSEState {
  events: SSEEvent[];
  connected: boolean;
  lastHeartbeat: number | null; // unix timestamp do último heartbeat
  error: string | null;
}

const MAX_BUFFER    = 100;
const BACKOFF_BASE  = 1000;  // 1s inicial
const BACKOFF_MAX   = 30000; // 30s máximo
const HEARTBEAT_TIMEOUT = 45000; // 45s — heartbeat a cada 15s no servidor

/**
 * useSSE: conecta ao endpoint SSE do API service Go.
 *
 * Reconexão automática com exponential backoff.
 * Buffer circular: mantém apenas os últimos MAX_BUFFER eventos.
 * Detecção de heartbeat perdido: connected=false se nenhum heartbeat em 45s.
 *
 * Usage:
 *   const { events, connected, lastHeartbeat } = useSSE("http://localhost:8080/api/v1/events");
 */
export function useSSE(url: string): SSEState {
  const [state, setState] = useState<SSEState>({
    events:        [],
    connected:     false,
    lastHeartbeat: null,
    error:         null,
  });

  const esRef          = useRef<EventSource | null>(null);
  const retryCount     = useRef(0);
  const retryTimerRef  = useRef<ReturnType<typeof setTimeout> | null>(null);
  const heartbeatTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isMounted      = useRef(true);

  const resetHeartbeatTimer = useCallback(() => {
    if (heartbeatTimer.current) clearTimeout(heartbeatTimer.current);
    heartbeatTimer.current = setTimeout(() => {
      if (isMounted.current) {
        setState((prev) => ({ ...prev, connected: false }));
      }
    }, HEARTBEAT_TIMEOUT);
  }, []);

  const connect = useCallback(() => {
    if (!isMounted.current) return;

    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => {
      if (!isMounted.current) return;
      retryCount.current = 0;
      setState((prev) => ({ ...prev, connected: true, error: null }));
      resetHeartbeatTimer();
    };

    es.addEventListener("heartbeat", () => {
      if (!isMounted.current) return;
      const now = Math.floor(Date.now() / 1000);
      setState((prev) => ({ ...prev, connected: true, lastHeartbeat: now }));
      resetHeartbeatTimer();
    });

    // Listener genérico para todos os event types do projeto
    const eventTypes = ["worker_status", "queue_lag", "healing", "token", "done", "error", "start"];
    for (const type of eventTypes) {
      es.addEventListener(type, (e: MessageEvent) => {
        if (!isMounted.current) return;

        let payload: Record<string, unknown> = {};
        try {
          payload = JSON.parse(e.data);
        } catch {
          payload = { raw: e.data };
        }

        const event: SSEEvent = { type, payload, receivedAt: Date.now() };

        setState((prev) => {
          // Buffer circular: manter últimos MAX_BUFFER
          const next = prev.events.length >= MAX_BUFFER
            ? [...prev.events.slice(-(MAX_BUFFER - 1)), event]
            : [...prev.events, event];
          return { ...prev, events: next };
        });
      });
    }

    es.onerror = () => {
      if (!isMounted.current) return;

      es.close();
      esRef.current = null;
      setState((prev) => ({ ...prev, connected: false }));

      // Exponential backoff com jitter
      const delay = Math.min(
        BACKOFF_BASE * Math.pow(2, retryCount.current) + Math.random() * 500,
        BACKOFF_MAX,
      );
      retryCount.current += 1;

      retryTimerRef.current = setTimeout(() => {
        if (isMounted.current) connect();
      }, delay);
    };
  }, [url, resetHeartbeatTimer]);

  useEffect(() => {
    isMounted.current = true;
    connect();

    return () => {
      isMounted.current = false;
      esRef.current?.close();
      if (retryTimerRef.current)  clearTimeout(retryTimerRef.current);
      if (heartbeatTimer.current) clearTimeout(heartbeatTimer.current);
    };
  }, [connect]);

  return state;
}
```

### Uso no componente
```tsx
"use client";

import { useSSE } from "@/hooks/useSSE";

export function MyComponent() {
  const { events, connected, lastHeartbeat, error } = useSSE(
    process.env.NEXT_PUBLIC_API_URL + "/api/v1/events"
  );

  // Filtrar apenas eventos relevantes
  const healingEvents = events.filter(e => e.type === "healing");

  return (
    <div>
      <span>{connected ? "Connected" : "Reconnecting..."}</span>
      {healingEvents.map((e, i) => (
        <div key={i}>{JSON.stringify(e.payload)}</div>
      ))}
    </div>
  );
}
```

## Comportamento de reconexão
| Tentativa | Delay (aprox) |
|---|---|
| 1 | 1s |
| 2 | 2s |
| 3 | 4s |
| 4 | 8s |
| 5+ | 16–30s |

## Checklist pós-geração
- [ ] `isMounted.current` verificado em todos os callbacks — sem setState após unmount
- [ ] `es.close()` chamado no cleanup — sem EventSource orphan
- [ ] `clearTimeout` nos dois timers no cleanup
- [ ] Buffer circular: nunca crescer além de `MAX_BUFFER`
- [ ] `HEARTBEAT_TIMEOUT = 45s` > heartbeat interval do servidor (15s) com folga
- [ ] Testar com `docker compose stop api-service` — deve mostrar "Reconnecting" e reconectar
- [ ] Testar com DevTools → Network → EventStream para verificar eventos chegando
