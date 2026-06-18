"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import type {
  SSEEvent,
  OperationsSummary,
  WorkerStatus,
  HealingEvent,
  HealingSSEEvent,
} from "@/lib/types";

const MAX_EVENTS = 100;
const MAX_HEALING_EVENTS = 50;
const SSE_URL = "http://localhost:8082/events";
const OPS_SUMMARY_URL = "http://localhost:8082/api/operations/summary";

const BACKOFF_INITIAL_MS = 1000;
const BACKOFF_MAX_MS = 30000;

interface SSEContextValue {
  events: SSEEvent[];
  connected: boolean;
  connectedAt: Date | null;
  reconnects: number;
  lastEventAt: Date | null;
  workers: WorkerStatus[];
  activeHealingEvents: HealingEvent[];
  recentHealingEvents: HealingEvent[];
  opsLoading: boolean;
}

const SSEContext = createContext<SSEContextValue>({
  events: [],
  connected: false,
  connectedAt: null,
  reconnects: 0,
  lastEventAt: null,
  workers: [],
  activeHealingEvents: [],
  recentHealingEvents: [],
  opsLoading: true,
});

export function useSSEContext() {
  return useContext(SSEContext);
}

// --- Runtime type guards (replace unsafe `as unknown as` casts) ---

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isHealingSSEEvent(
  data: Record<string, unknown>
): data is Record<string, unknown> & HealingSSEEvent {
  return (
    typeof data.event_type === "string" &&
    data.event_type.startsWith("healing.") &&
    typeof data.healing_event_id === "string" &&
    typeof data.source === "string" &&
    typeof data.timestamp === "string" &&
    (data.severity === "info" ||
      data.severity === "warning" ||
      data.severity === "critical") &&
    (data.status === "active" || data.status === "resolved")
  );
}

function isTaskSSEEvent(
  data: Record<string, unknown>
): data is Record<string, unknown> & SSEEvent {
  return (
    typeof data.event_id === "string" &&
    typeof data.event_type === "string" &&
    typeof data.task_id === "string" &&
    typeof data.trace_id === "string" &&
    typeof data.traceparent === "string" &&
    typeof data.timestamp === "string" &&
    typeof data.source === "string"
  );
}

function isOperationsSummary(data: unknown): data is OperationsSummary {
  if (!isRecord(data)) return false;
  return (
    typeof data.timestamp === "string" &&
    Array.isArray(data.workers) &&
    Array.isArray(data.active_events) &&
    Array.isArray(data.recent_events)
  );
}

export function SSEProvider({ children }: { children: React.ReactNode }) {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [connectedAt, setConnectedAt] = useState<Date | null>(null);
  const [reconnects, setReconnects] = useState(0);
  const [lastEventAt, setLastEventAt] = useState<Date | null>(null);
  const [workers, setWorkers] = useState<WorkerStatus[]>([]);
  const [activeHealingEvents, setActiveHealingEvents] = useState<
    HealingEvent[]
  >([]);
  const [recentHealingEvents, setRecentHealingEvents] = useState<
    HealingEvent[]
  >([]);
  const [opsLoading, setOpsLoading] = useState(true);
  const esRef = useRef<EventSource | null>(null);
  // Tracks whether the EventSource has opened at least once.
  // Used to distinguish initial connection from browser auto-reconnections.
  const hasConnectedRef = useRef(false);
  // Exponential backoff state -- refs to avoid re-renders
  const backoffDelayRef = useRef(BACKOFF_INITIAL_MS);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(
    null
  );

  useEffect(() => {
    let cancelled = false;
    async function fetchSummary() {
      try {
        const res = await fetch(OPS_SUMMARY_URL);
        if (!res.ok) return;
        const data: unknown = await res.json();
        if (cancelled) return;
        if (isOperationsSummary(data)) {
          setWorkers(data.workers);
          setActiveHealingEvents(data.active_events);
          setRecentHealingEvents(data.recent_events);
        }
      } catch {
        // bootstrap fetch failed -- SSE will fill in state incrementally
      } finally {
        if (!cancelled) setOpsLoading(false);
      }
    }
    fetchSummary();
    return () => {
      cancelled = true;
    };
  }, []);

  const handleHealingEvent = useCallback((data: HealingSSEEvent) => {
    const healingEvent: HealingEvent = {
      id: data.healing_event_id,
      event_type: data.event_type
        .replace(/^healing\./, "")
        .replace(/\.resolved$/, ""),
      severity: data.severity,
      source: data.source,
      status: data.status,
      details: data.details ?? {},
      created_at: data.timestamp,
      resolved_at: data.status === "resolved" ? data.timestamp : undefined,
    };

    if (data.status === "resolved") {
      setActiveHealingEvents((prev) =>
        prev.filter((e) => e.id !== data.healing_event_id)
      );
    } else {
      setActiveHealingEvents((prev) =>
        [
          healingEvent,
          ...prev.filter((e) => e.id !== data.healing_event_id),
        ].slice(0, MAX_HEALING_EVENTS)
      );
    }

    setRecentHealingEvents((prev) =>
      [
        healingEvent,
        ...prev.filter((e) => e.id !== data.healing_event_id),
      ].slice(0, MAX_HEALING_EVENTS)
    );
  }, []);

  const scheduleReconnect = useCallback(() => {
    // Clear any existing scheduled reconnect
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    const delay = backoffDelayRef.current;
    reconnectTimeoutRef.current = setTimeout(() => {
      reconnectTimeoutRef.current = null;
      // Double the delay for next failure, capped at max
      backoffDelayRef.current = Math.min(delay * 2, BACKOFF_MAX_MS);
      connectRef.current();
    }, delay);
  }, []);

  const connect = useCallback(() => {
    // Clear any pending reconnect timeout before opening a new connection
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    if (esRef.current) {
      esRef.current.close();
    }

    const es = new EventSource(SSE_URL);
    esRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setConnectedAt(new Date());
      backoffDelayRef.current = BACKOFF_INITIAL_MS;
      if (hasConnectedRef.current) {
        setReconnects((n) => n + 1);
      }
      hasConnectedRef.current = true;
    };

    es.onmessage = (e: MessageEvent<string>) => {
      try {
        const raw: unknown = JSON.parse(e.data);
        if (!isRecord(raw)) return;

        setLastEventAt(new Date());

        if (isHealingSSEEvent(raw)) {
          handleHealingEvent(raw);
        } else if (isTaskSSEEvent(raw)) {
          setEvents((prev) => [raw, ...prev].slice(0, MAX_EVENTS));
        }
        // unknown event shape -- silently ignore
      } catch {
        // ignore malformed JSON
      }
    };

    es.onerror = () => {
      setConnected(false);
      // Close the native EventSource to prevent its built-in auto-reconnect.
      // We manage reconnection manually with exponential backoff.
      es.close();
      esRef.current = null;
      scheduleReconnect();
    };
  }, [handleHealingEvent, scheduleReconnect]);

  // Stable ref so scheduleReconnect's timeout can always call the latest connect
  const connectRef = useRef(connect);
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  useEffect(() => {
    connect();
    return () => {
      // Clean up EventSource
      esRef.current?.close();
      esRef.current = null;
      // Clean up any pending reconnect timeout
      if (reconnectTimeoutRef.current !== null) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
    };
  }, [connect]);

  return (
    <SSEContext.Provider
      value={{
        events,
        connected,
        connectedAt,
        reconnects,
        lastEventAt,
        workers,
        activeHealingEvents,
        recentHealingEvents,
        opsLoading,
      }}
    >
      {children}
    </SSEContext.Provider>
  );
}
