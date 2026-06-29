"use client";

import React from "react";
import {
  Cpu,
  Inbox,
  AlertTriangle,
  AlertOctagon,
  CheckCircle2,
} from "lucide-react";
import { useSSEContext } from "@/components/providers/sse-provider";

function StatusDot({ color }: { color: "green" | "amber" | "red" | "zinc" }) {
  const colorMap = {
    green: "bg-emerald-400",
    amber: "bg-amber-400",
    red: "bg-red-400",
    zinc: "bg-zinc-500",
  };
  return (
    <span className={`inline-block size-2.5 rounded-full ${colorMap[color]}`} />
  );
}

interface StatusCardProps {
  icon: React.ReactNode;
  label: string;
  statusColor: "green" | "amber" | "red" | "zinc";
  children: React.ReactNode;
  accentBar?: string;
}

function StatusCard({ icon, label, statusColor, children, accentBar = "bg-zinc-700" }: StatusCardProps) {
  return (
    <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden">
      <div className={`absolute top-0 left-0 right-0 h-[2px] ${accentBar}`} />
      <div className="flex items-center gap-2 text-zinc-400 mb-3">
        {icon}
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="flex items-center gap-3">
        <StatusDot color={statusColor} />
        {children}
      </div>
    </div>
  );
}

export const StatusCards = React.memo(function StatusCards() {
  const { workers, activeHealingEvents } = useSSEContext();

  const healthyCount = workers.filter((w) => w.status === "healthy").length;
  const staleCount = workers.filter((w) => w.status === "stale").length;
  const totalWorkers = workers.length;

  const hasQueueLag = activeHealingEvents.some(
    (e) => e.event_type === "queue.lag"
  );
  const dlqEvent = activeHealingEvents.find(
    (e) => e.event_type === "dlq.nonempty"
  );
  const dlqDepth = dlqEvent
    ? Number(dlqEvent.details?.depth ?? 0)
    : 0;

  const workerStatusColor: "green" | "amber" | "red" | "zinc" =
    totalWorkers === 0 ? "zinc" : staleCount > 0 ? "amber" : "green";

  const incidentCount = activeHealingEvents.length;

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      {/* Workers */}
      <StatusCard
        icon={<Cpu className="size-4" />}
        label="Workers"
        statusColor={workerStatusColor}
        accentBar={workerStatusColor === "green" ? "bg-emerald-500" : workerStatusColor === "amber" ? "bg-amber-500" : "bg-zinc-700"}
      >
        {totalWorkers === 0 ? (
          <span className="text-sm text-zinc-500">No workers</span>
        ) : (
          <div className="flex items-baseline gap-1.5">
            <span className="text-2xl font-bold text-emerald-400 tabular-nums">
              {healthyCount}
            </span>
            <span className="text-zinc-500">/</span>
            <span className="text-zinc-400">{totalWorkers}</span>
            {staleCount > 0 && (
              <span className="text-xs text-orange-400 ml-1">
                {staleCount} stale
              </span>
            )}
          </div>
        )}
      </StatusCard>

      {/* Queue */}
      <StatusCard
        icon={<Inbox className="size-4" />}
        label="Queue"
        statusColor={hasQueueLag ? "red" : "green"}
        accentBar={hasQueueLag ? "bg-red-500" : "bg-emerald-500"}
      >
        <span
          className={`text-lg font-semibold ${hasQueueLag ? "text-red-400" : "text-emerald-400"}`}
        >
          {hasQueueLag ? "Lag Detected" : "Healthy"}
        </span>
      </StatusCard>

      {/* DLQ */}
      <StatusCard
        icon={<AlertTriangle className="size-4" />}
        label="DLQ"
        statusColor={dlqDepth > 0 ? "red" : "green"}
        accentBar={dlqDepth > 0 ? "bg-red-500" : "bg-emerald-500"}
      >
        <div className="flex items-baseline gap-2">
          <span
            className={`text-2xl font-bold tabular-nums ${dlqDepth > 0 ? "text-red-400" : "text-emerald-400"}`}
          >
            {dlqDepth}
          </span>
          {dlqDepth === 0 && (
            <span className="flex items-center gap-1 text-xs text-zinc-500">
              <CheckCircle2 className="size-3 text-emerald-500" />
              empty
            </span>
          )}
        </div>
      </StatusCard>

      {/* Incidents */}
      <StatusCard
        icon={<AlertOctagon className="size-4" />}
        label="Incidents"
        statusColor={incidentCount > 0 ? "amber" : "green"}
        accentBar={incidentCount > 0 ? "bg-amber-500" : "bg-emerald-500"}
      >
        <div className="flex items-baseline gap-2">
          <span
            className={`text-2xl font-bold tabular-nums ${incidentCount > 0 ? "text-amber-400" : "text-emerald-400"}`}
          >
            {incidentCount}
          </span>
          {incidentCount === 0 && (
            <span className="flex items-center gap-1 text-xs text-zinc-500">
              <CheckCircle2 className="size-3 text-emerald-500" />
              clear
            </span>
          )}
        </div>
      </StatusCard>
    </div>
  );
});
