"use client";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";

export function EventFeed() {
  const { events, connected, reconnects, lastEventAt } = useSSEContext();

  const statusLabel = connected ? "connected" : "disconnected";
  const statusColor = connected ? "text-green-400" : "text-red-400";

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
            Event Stream
          </CardTitle>
          <div className="flex items-center gap-3 text-xs font-mono">
            <span className={statusColor}>● {statusLabel}</span>
            {reconnects > 0 && (
              <span className="text-zinc-500">reconnects: {reconnects}</span>
            )}
            {lastEventAt && (
              <span className="text-zinc-500">
                last: {lastEventAt.toLocaleTimeString()}
              </span>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className="text-zinc-500 text-sm font-mono py-4 text-center">
            waiting for events…
          </p>
        ) : (
          <ul className="space-y-2 max-h-[480px] overflow-y-auto">
            {events.map((ev) => (
              <li
                key={ev.event_id}
                className="border border-zinc-700 rounded-md p-3 bg-zinc-800 text-xs font-mono"
              >
                <div className="flex items-center gap-2 mb-1">
                  <Badge
                    variant="outline"
                    className="text-emerald-400 border-emerald-600 text-[10px] uppercase"
                  >
                    {ev.event_type}
                  </Badge>
                  <span className="text-zinc-400">{ev.timestamp}</span>
                </div>
                <div className="text-zinc-300 truncate">
                  <span className="text-zinc-500">task_id </span>
                  {ev.task_id}
                </div>
                <div className="text-zinc-300 truncate">
                  <span className="text-zinc-500">trace_id </span>
                  {ev.trace_id}
                </div>
                <div className="text-zinc-300">
                  <span className="text-zinc-500">source </span>
                  {ev.source}
                </div>
                {ev.execution_status && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">execution_status </span>
                    {ev.execution_status}
                  </div>
                )}
                {ev.validation_status && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">validation_status </span>
                    {ev.validation_status}
                  </div>
                )}
                {ev.model && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">model </span>
                    {ev.model}
                  </div>
                )}
                {ev.inference_duration_ms > 0 && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">inference_duration_ms </span>
                    {ev.inference_duration_ms}
                  </div>
                )}
                {ev.error_reason && (
                  <div className="text-red-400">
                    <span className="text-zinc-500">error_reason </span>
                    {ev.error_reason}
                  </div>
                )}
                {ev.output && (
                  <div className="mt-2 border-t border-zinc-700 pt-2">
                    <span className="text-zinc-500 block mb-1">output</span>
                    <pre className="text-zinc-200 whitespace-pre-wrap break-words text-[11px] max-h-48 overflow-y-auto">
                      {ev.output}
                    </pre>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
