---
name: frontend:event-dashboard
description: Dashboard operacional Next.js 14+ (App Router) com feed SSE de eventos, status de workers, queue lag e link para trace no Grafana Tempo. Padrão visual operacional (não chat UI). Usa shadcn/ui + Tailwind. Sem estado global — tudo derivado do feed SSE.
---

# Skill: frontend:event-dashboard

## Input necessário
1. URL do API service (ex: `http://localhost:8080`)
2. URL do Grafana (ex: `http://localhost:3000`) — para links de trace
3. Quais painéis: workers, queue lag, healing events, trace viewer?

## O que gerar

### `app/dashboard/page.tsx` (Server Component wrapper)
```tsx
import { DashboardClient } from "@/components/dashboard/DashboardClient";

export const metadata = { title: "EventDrive — Operational Dashboard" };

export default function DashboardPage() {
  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100 p-6">
      <div className="max-w-7xl mx-auto space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-mono font-semibold text-zinc-100">
              EventDrive Runtime
            </h1>
            <p className="text-sm text-zinc-500">Local-first AI event platform</p>
          </div>
          <span className="text-xs font-mono text-zinc-600">
            {new Date().toISOString().split("T")[0]}
          </span>
        </header>
        <DashboardClient apiUrl={process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"} />
      </div>
    </main>
  );
}
```

### `components/dashboard/DashboardClient.tsx`
```tsx
"use client";

import { useSSE } from "@/hooks/useSSE";
import { WorkerStatusPanel } from "./WorkerStatusPanel";
import { QueueLagPanel } from "./QueueLagPanel";
import { EventFeed } from "./EventFeed";
import { HealingPanel } from "./HealingPanel";

interface Props {
  apiUrl: string;
}

export function DashboardClient({ apiUrl }: Props) {
  const { events, connected, lastHeartbeat } = useSSE(`${apiUrl}/api/v1/events`);

  // Derivar estado dos eventos — sem useState separado para cada painel
  const workerEvents = events.filter((e) => e.type === "worker_status");
  const queueEvents  = events.filter((e) => e.type === "queue_lag");
  const healingEvents = events.filter((e) => e.type === "healing");

  // Estado mais recente de cada worker (reduzido do feed)
  const workerStatus = workerEvents.reduce<Record<string, unknown>>((acc, e) => {
    const p = e.payload as { worker_id: string; status: string };
    acc[p.worker_id] = p;
    return acc;
  }, {});

  return (
    <div className="space-y-4">
      {/* Connection status bar */}
      <div className="flex items-center gap-2 text-xs font-mono text-zinc-500">
        <span
          className={`w-2 h-2 rounded-full ${
            connected ? "bg-emerald-500 animate-pulse" : "bg-red-500"
          }`}
        />
        {connected ? "SSE connected" : "SSE disconnected — reconnecting..."}
        {lastHeartbeat && (
          <span className="ml-auto">
            last heartbeat: {new Date(lastHeartbeat * 1000).toLocaleTimeString()}
          </span>
        )}
      </div>

      {/* Grid operacional — não chat UI */}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        <WorkerStatusPanel workerStatus={workerStatus} />
        <QueueLagPanel events={queueEvents} />
        <HealingPanel events={healingEvents} />
      </div>

      {/* Event feed — últimos 100 eventos */}
      <EventFeed events={events} />
    </div>
  );
}
```

### `components/dashboard/WorkerStatusPanel.tsx`
```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

interface WorkerState {
  worker_id: string;
  status: "alive" | "stale" | "offline" | "restarting";
}

export function WorkerStatusPanel({ workerStatus }: { workerStatus: Record<string, WorkerState> }) {
  const workers = Object.values(workerStatus);

  return (
    <Card className="bg-zinc-900 border-zinc-800">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-mono text-zinc-400">Workers</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {workers.length === 0 ? (
          <p className="text-xs text-zinc-600">No worker heartbeats received yet</p>
        ) : (
          workers.map((w) => (
            <div key={w.worker_id} className="flex items-center justify-between">
              <span className="text-xs font-mono text-zinc-300">{w.worker_id}</span>
              <Badge
                variant="outline"
                className={
                  w.status === "alive"     ? "border-emerald-700 text-emerald-400" :
                  w.status === "stale"     ? "border-yellow-700 text-yellow-400" :
                  w.status === "restarting"? "border-blue-700 text-blue-400" :
                                             "border-red-700 text-red-400"
                }
              >
                {w.status}
              </Badge>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}
```

### `components/dashboard/EventFeed.tsx`
```tsx
import { SSEEvent } from "@/hooks/useSSE";

const EVENT_COLORS: Record<string, string> = {
  worker_status: "text-blue-400",
  queue_lag:     "text-yellow-400",
  healing:       "text-orange-400",
  heartbeat:     "text-zinc-600",
};

export function EventFeed({ events }: { events: SSEEvent[] }) {
  // Mostrar apenas últimos 50 eventos não-heartbeat no feed
  const displayEvents = events
    .filter((e) => e.type !== "heartbeat")
    .slice(-50)
    .reverse();

  return (
    <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
      <h3 className="text-sm font-mono text-zinc-400 mb-3">Event Feed</h3>
      <div className="space-y-1 max-h-64 overflow-y-auto font-mono text-xs">
        {displayEvents.map((e, i) => (
          <div key={i} className="flex gap-3 text-zinc-500">
            <span className="text-zinc-700 shrink-0">
              {new Date(e.receivedAt).toLocaleTimeString()}
            </span>
            <span className={EVENT_COLORS[e.type] ?? "text-zinc-400"}>{e.type}</span>
            {e.payload?.trace_id && (
              <a
                href={`http://localhost:3000/explore?orgId=1&left=%7B"datasource":"tempo","queries":[%7B"query":"${e.payload.trace_id}"%7D]%7D`}
                target="_blank"
                rel="noopener noreferrer"
                className="text-zinc-700 hover:text-zinc-400 ml-auto"
              >
                trace ↗
              </a>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
```

## Checklist pós-geração
- [ ] `NEXT_PUBLIC_API_URL` definido em `.env.local`
- [ ] `useSSE` hook importado corretamente (usar `frontend:sse-hook` se não existir)
- [ ] Nenhum `useState` separado para dados derivados do SSE — reduzir do feed
- [ ] Sem polling HTTP — apenas SSE
- [ ] Link de trace aponta para Grafana Tempo com query correta
- [ ] `npm run build` sem erros TypeScript
- [ ] Visual: fundo escuro, font mono, sem chat bubbles ou avatars
