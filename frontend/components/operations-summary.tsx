"use client";

import React from "react";
import { AlertTriangle, AlertCircle, Info, Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import type { HealingEvent } from "@/lib/types";

function timeAgo(dateStr: string): string {
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 0) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

function SeverityIcon({ severity }: { severity: HealingEvent["severity"] }) {
  switch (severity) {
    case "critical":
      return <AlertCircle className="size-4 text-red-400" />;
    case "warning":
      return <AlertTriangle className="size-4 text-amber-400" />;
    case "info":
      return <Info className="size-4 text-blue-400" />;
  }
}

const OperationsSummaryInner = React.memo(function OperationsSummaryInner() {
  const { workers, activeHealingEvents, opsLoading } = useSSEContext();

  if (opsLoading) {
    return (
      <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
            Operations
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center gap-2 py-4 text-zinc-500 text-sm font-mono">
            <Loader2 className="size-4 animate-spin" />
            loading operational state...
          </div>
        </CardContent>
      </Card>
    );
  }

  const healthyCount = workers.filter((w) => w.status === "healthy").length;
  const staleCount = workers.filter((w) => w.status === "stale").length;

  const hasQueueLag = activeHealingEvents.some(
    (e) => e.event_type === "queue.lag"
  );
  const hasDlqNonempty = activeHealingEvents.some(
    (e) => e.event_type === "dlq.nonempty"
  );

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
          Operations
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs font-mono mb-3">
          <div className="flex flex-col gap-1">
            <span className="text-zinc-500">Workers</span>
            <div className="flex items-center gap-2">
              {healthyCount > 0 && (
                <span className="text-green-400">{healthyCount} healthy</span>
              )}
              {staleCount > 0 && (
                <span className="text-amber-400">{staleCount} stale</span>
              )}
              {workers.length === 0 && (
                <span className="text-zinc-500">none</span>
              )}
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-zinc-500">Active Incidents</span>
            <span
              className={
                activeHealingEvents.length > 0
                  ? "text-amber-400"
                  : "text-green-400"
              }
            >
              {activeHealingEvents.length}
            </span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-zinc-500">Queue</span>
            <span className={hasQueueLag ? "text-red-400" : "text-green-400"}>
              {hasQueueLag ? "lag" : "ok"}
            </span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-zinc-500">DLQ</span>
            <span
              className={hasDlqNonempty ? "text-red-400" : "text-green-400"}
            >
              {hasDlqNonempty ? "nonempty" : "ok"}
            </span>
          </div>
        </div>

        {activeHealingEvents.length > 0 && (
          <ul className="space-y-1.5 border-t border-zinc-700 pt-3">
            {activeHealingEvents.map((ev) => (
              <li
                key={ev.id}
                className="flex items-center gap-2 text-xs font-mono text-zinc-300"
              >
                <SeverityIcon severity={ev.severity} />
                <Badge
                  variant="outline"
                  className={severityBadgeClass(ev.severity)}
                >
                  {ev.event_type}
                </Badge>
                {ev.worker_id && (
                  <span className="text-zinc-500 truncate max-w-[120px]">
                    {ev.worker_id.slice(0, 12)}
                  </span>
                )}
                <span className="ml-auto text-zinc-500 shrink-0">
                  {timeAgo(ev.created_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
});

function severityBadgeClass(
  severity: HealingEvent["severity"]
): string {
  switch (severity) {
    case "critical":
      return "text-red-400 border-red-600 text-[10px] uppercase";
    case "warning":
      return "text-amber-400 border-amber-600 text-[10px] uppercase";
    case "info":
      return "text-blue-400 border-blue-600 text-[10px] uppercase";
  }
}

export { OperationsSummaryInner as OperationsSummary, timeAgo, severityBadgeClass, SeverityIcon };
