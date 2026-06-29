"use client";

import React from "react";
import {
  AlertTriangle,
  AlertCircle,
  Info,
  Loader2,
  Activity,
  Cpu,
  Inbox,
  AlertOctagon,
  CheckCircle2,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import type { HealingEvent } from "@/lib/types";

function timeAgo(dateStr: string): string {
  const seconds = Math.floor(
    (Date.now() - new Date(dateStr).getTime()) / 1000
  );
  if (seconds < 0) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

function displayEventType(eventType: string): string {
  const map: Record<string, string> = {
    "worker.stale": "Heartbeat Lost",
    "worker.down": "Worker Offline",
    "task.stuck": "Task Frozen",
    "queue.lag": "Queue Overload",
    "dlq.nonempty": "DLQ",
  };
  return map[eventType] ?? eventType;
}

function SeverityIcon({ severity }: { severity: HealingEvent["severity"] }) {
  switch (severity) {
    case "critical":
      return <AlertCircle className="size-4 text-red-400" />;
    case "warning":
      return <AlertTriangle className="size-4 text-orange-400" />;
    case "info":
      return <Info className="size-4 text-blue-400" />;
  }
}

function severityBadgeClass(severity: HealingEvent["severity"]): string {
  switch (severity) {
    case "critical":
      return "text-red-400 border-red-500/40 bg-red-500/10 text-xs uppercase";
    case "warning":
      return "text-orange-400 border-orange-500/40 bg-orange-500/10 text-xs uppercase";
    case "info":
      return "text-blue-400 border-blue-500/40 bg-blue-500/10 text-xs uppercase";
  }
}

function StatusDot({ color }: { color: "green" | "amber" | "red" | "zinc" }) {
  const colorMap = {
    green: "bg-emerald-400 shadow-emerald-400/50",
    amber: "bg-amber-400 shadow-amber-400/50",
    red: "bg-red-400 shadow-red-400/50",
    zinc: "bg-zinc-500 shadow-zinc-500/50",
  };
  return (
    <span
      className={`inline-block size-2.5 rounded-full shadow-sm ${colorMap[color]}`}
    />
  );
}

const OperationsSummaryInner = React.memo(function OperationsSummaryInner() {
  const { workers, activeHealingEvents, opsLoading } = useSSEContext();

  if (opsLoading) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-emerald-500/30 border-t-2">
        <CardHeader>
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Activity className="size-5 text-emerald-400" />
            Operations
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center gap-3 py-10 text-zinc-500 text-base">
            <Loader2 className="size-5 animate-spin" />
            Loading operational state...
          </div>
        </CardContent>
      </Card>
    );
  }

  const healthyCount = workers.filter((w) => w.status === "healthy").length;
  const staleCount = workers.filter((w) => w.status === "stale").length;
  const totalWorkers = workers.length;

  const hasQueueLag = activeHealingEvents.some(
    (e) => e.event_type === "queue.lag"
  );
  const hasDlqNonempty = activeHealingEvents.some(
    (e) => e.event_type === "dlq.nonempty"
  );

  const workerStatusColor: "green" | "amber" | "red" | "zinc" =
    totalWorkers === 0 ? "zinc" : staleCount > 0 ? "amber" : "green";

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-emerald-500/30 border-t-2">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Activity className="size-5 text-emerald-400" />
            Operations
          </CardTitle>
          {activeHealingEvents.length === 0 && totalWorkers > 0 && (
            <div className="flex items-center gap-2 text-sm text-emerald-400/90 font-medium">
              <CheckCircle2 className="size-4" />
              All systems nominal
            </div>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-5">
          {/* Workers */}
          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-5 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-3">
              <Cpu className="size-4" />
              <span className="text-sm font-medium">Workers</span>
            </div>
            <div className="flex items-center gap-3">
              <StatusDot color={workerStatusColor} />
              {totalWorkers === 0 ? (
                <span className="text-base text-zinc-500">No workers</span>
              ) : (
                <div className="flex items-center gap-2">
                  <span className="text-3xl font-bold text-emerald-400 tabular-nums tracking-tight">
                    {healthyCount}
                  </span>
                  <span className="text-lg text-zinc-500">/</span>
                  <span className="text-lg text-zinc-400">{totalWorkers}</span>
                  {staleCount > 0 && (
                    <Badge
                      variant="outline"
                      className="text-orange-400 border-orange-500/40 bg-orange-500/10 text-xs ml-1"
                    >
                      {staleCount} stale
                    </Badge>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Incidents */}
          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-5 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-3">
              <AlertOctagon className="size-4" />
              <span className="text-sm font-medium">Incidents</span>
            </div>
            <div className="flex items-center gap-3">
              <StatusDot
                color={activeHealingEvents.length > 0 ? "amber" : "green"}
              />
              <span
                className={`text-3xl font-bold tabular-nums tracking-tight ${activeHealingEvents.length > 0 ? "text-amber-400" : "text-emerald-400"}`}
              >
                {activeHealingEvents.length}
              </span>
              <span className="text-sm text-zinc-400">active</span>
            </div>
          </div>

          {/* Queue */}
          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-5 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-3">
              <Inbox className="size-4" />
              <span className="text-sm font-medium">Queue</span>
            </div>
            <div className="flex items-center gap-3">
              <StatusDot color={hasQueueLag ? "red" : "green"} />
              <span
                className={`text-lg font-semibold ${hasQueueLag ? "text-red-400" : "text-emerald-400"}`}
              >
                {hasQueueLag ? "Lag Detected" : "Healthy"}
              </span>
            </div>
          </div>

          {/* DLQ */}
          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-5 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-3">
              <AlertTriangle className="size-4" />
              <span className="text-sm font-medium">DLQ</span>
            </div>
            <div className="flex items-center gap-3">
              <StatusDot color={hasDlqNonempty ? "red" : "green"} />
              <span
                className={`text-lg font-semibold ${hasDlqNonempty ? "text-red-400" : "text-emerald-400"}`}
              >
                {hasDlqNonempty ? "Has Messages" : "Empty"}
              </span>
            </div>
          </div>
        </div>

        {activeHealingEvents.length > 0 && (
          <div className="border-t border-zinc-800/80 pt-4">
            <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
              Active Incidents
            </h3>
            <ul className="space-y-2">
              {activeHealingEvents.map((ev) => (
                <li
                  key={ev.id}
                  className="flex items-center gap-3 rounded-lg border border-zinc-800/80 bg-zinc-800/30 px-4 py-3 transition-colors hover:border-zinc-700/80"
                >
                  <SeverityIcon severity={ev.severity} />
                  <Badge
                    variant="outline"
                    className={severityBadgeClass(ev.severity)}
                  >
                    {displayEventType(ev.event_type)}
                  </Badge>
                  {ev.worker_id && (
                    <span
                      className="text-sm text-zinc-400 font-mono truncate max-w-[120px]"
                      title={ev.worker_id}
                    >
                      {ev.worker_id.slice(0, 12)}
                    </span>
                  )}
                  <span className="ml-auto text-sm text-zinc-400">
                    {timeAgo(ev.created_at)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
});

export {
  OperationsSummaryInner as OperationsSummary,
  timeAgo,
  displayEventType,
  severityBadgeClass,
  SeverityIcon,
};
