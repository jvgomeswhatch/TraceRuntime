"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import type { SSEEvent } from "@/lib/types";

const MAX_EVENTS = 100;
const SSE_URL = "http://localhost:8082/events";

interface SSEContextValue {
  events: SSEEvent[];
  connected: boolean;
  reconnects: number;
  lastEventAt: Date | null;
}

const SSEContext = createContext<SSEContextValue>({
  events: [],
  connected: false,
  reconnects: 0,
  lastEventAt: null,
});

export function useSSEContext() {
  return useContext(SSEContext);
}

export function SSEProvider({ children }: { children: React.ReactNode }) {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [reconnects, setReconnects] = useState(0);
  const [lastEventAt, setLastEventAt] = useState<Date | null>(null);
  const esRef = useRef<EventSource | null>(null);

  const connect = useCallback((isReconnect: boolean) => {
    if (esRef.current) {
      esRef.current.close();
    }

    const es = new EventSource(SSE_URL);
    esRef.current = es;

    es.onopen = () => {
      setConnected(true);
      if (isReconnect) {
        setReconnects((n) => n + 1);
      }
    };

    es.onmessage = (e: MessageEvent<string>) => {
      try {
        const parsed: SSEEvent = JSON.parse(e.data);
        setLastEventAt(new Date());
        // bounded: keep only the last MAX_EVENTS, newest first
        setEvents((prev) => [parsed, ...prev].slice(0, MAX_EVENTS));
      } catch {
        // ignore malformed messages
      }
    };

    es.onerror = () => {
      setConnected(false);
      // browser will auto-reconnect; we track it on next onopen
    };
  }, []);

  useEffect(() => {
    connect(false);
    return () => {
      esRef.current?.close();
      esRef.current = null;
    };
  }, [connect]);

  return (
    <SSEContext.Provider value={{ events, connected, reconnects, lastEventAt }}>
      {children}
    </SSEContext.Provider>
  );
}
