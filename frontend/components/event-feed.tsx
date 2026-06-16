"use client";

import React, { useState, useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import { timeAgo, severityBadgeClass, SeverityIcon } from "@/components/operations-summary";
import type { SSEEvent, HealingEvent } from "@/lib/types";

type TabKey = "tasks" | "operations" | "all";

interface MergedItem {
  kind: "task" | "healing";
  timestamp: string;
  key: string;
  task?: SSEEvent;
  healing?: HealingEvent;
}

const TABS: { key: TabKey; label: string }[] = [
  { key: "tasks", label: "Tasks" },
  { key: "operations", label: "Operations" },
  { key: "all", label: "All" },
];

const TaskEventItem = React.memo(function TaskEventItem({
  ev,
}: {
  ev: SSEEvent;
}) {
  return (
    <li className="border border-zinc-700 rounded-md p-3 bg-zinc-800 text-xs font-mono">
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
      {ev.inference_duration_ms != null && ev.inference_duration_ms > 0 && (
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
  );
});

const HealingEventItem = React.memo(function HealingEventItem({
  ev,
}: {
  ev: HealingEvent;
}) {
  const statusColor =
    ev.status === "active"
      ? "text-red-400 border-red-600"
      : "text-green-400 border-green-600";

  const detailsSummary = Object.entries(ev.details)
    .slice(0, 3)
    .map(([k, v]) => `${k}=${String(v)}`)
    .join(" ");

  return (
    <li className="border border-zinc-700 rounded-md p-3 bg-zinc-800 text-xs font-mono">
      <div className="flex items-center gap-2 mb-1 flex-wrap">
        <SeverityIcon severity={ev.severity} />
        <Badge
          variant="outline"
          className={severityBadgeClass(ev.severity)}
        >
          {ev.event_type}
        </Badge>
        <Badge
          variant="outline"
          className={`${statusColor} text-[10px] uppercase`}
        >
          {ev.status}
        </Badge>
        <span className="text-zinc-500 ml-auto shrink-0">
          {timeAgo(ev.created_at)}
        </span>
      </div>
      {ev.worker_id && (
        <div className="text-zinc-300 truncate">
          <span className="text-zinc-500">worker_id </span>
          {ev.worker_id}
        </div>
      )}
      <div className="text-zinc-300">
        <span className="text-zinc-500">source </span>
        {ev.source}
      </div>
      {detailsSummary && (
        <div className="text-zinc-400 truncate mt-1">
          {detailsSummary}
        </div>
      )}
      {ev.resolved_at && (
        <div className="text-green-400 mt-1">
          <span className="text-zinc-500">resolved </span>
          {timeAgo(ev.resolved_at)}
        </div>
      )}
    </li>
  );
});

export function EventFeed() {
  const { events, connected, reconnects, lastEventAt, recentHealingEvents } =
    useSSEContext();
  const [activeTab, setActiveTab] = useState<TabKey>("tasks");

  const statusLabel = connected ? "connected" : "disconnected";
  const statusColor = connected ? "text-green-400" : "text-red-400";

  const mergedItems = useMemo<MergedItem[]>(() => {
    if (activeTab !== "all") return [];

    const taskItems: MergedItem[] = events.map((ev) => ({
      kind: "task" as const,
      timestamp: ev.timestamp,
      key: `task-${ev.event_id}`,
      task: ev,
    }));

    const healingItems: MergedItem[] = recentHealingEvents.map((ev) => ({
      kind: "healing" as const,
      timestamp: ev.created_at,
      key: `healing-${ev.id}`,
      healing: ev,
    }));

    return [...taskItems, ...healingItems].sort(
      (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    );
  }, [activeTab, events, recentHealingEvents]);

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
            Event Stream
          </CardTitle>
          <div className="flex items-center gap-3 text-xs font-mono">
            <span className={statusColor}>&#9679; {statusLabel}</span>
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

        <div className="flex gap-1 mt-2">
          {TABS.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`px-3 py-1 text-xs font-mono rounded-md transition-colors ${
                activeTab === tab.key
                  ? "bg-zinc-700 text-zinc-100"
                  : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800"
              }`}
            >
              {tab.label}
              {tab.key === "operations" && recentHealingEvents.length > 0 && (
                <span className="ml-1.5 text-amber-400">
                  {recentHealingEvents.length}
                </span>
              )}
            </button>
          ))}
        </div>
      </CardHeader>

      <CardContent>
        {activeTab === "tasks" && (
          <>
            {events.length === 0 ? (
              <p className="text-zinc-500 text-sm font-mono py-4 text-center">
                waiting for events...
              </p>
            ) : (
              <ul className="space-y-2 max-h-[480px] overflow-y-auto">
                {events.map((ev) => (
                  <TaskEventItem key={ev.event_id} ev={ev} />
                ))}
              </ul>
            )}
          </>
        )}

        {activeTab === "operations" && (
          <>
            {recentHealingEvents.length === 0 ? (
              <p className="text-zinc-500 text-sm font-mono py-4 text-center">
                no healing events yet...
              </p>
            ) : (
              <ul className="space-y-2 max-h-[480px] overflow-y-auto">
                {recentHealingEvents.map((ev) => (
                  <HealingEventItem key={ev.id} ev={ev} />
                ))}
              </ul>
            )}
          </>
        )}

        {activeTab === "all" && (
          <>
            {mergedItems.length === 0 ? (
              <p className="text-zinc-500 text-sm font-mono py-4 text-center">
                waiting for events...
              </p>
            ) : (
              <ul className="space-y-2 max-h-[480px] overflow-y-auto">
                {mergedItems.map((item) =>
                  item.kind === "task" && item.task ? (
                    <TaskEventItem key={item.key} ev={item.task} />
                  ) : item.healing ? (
                    <HealingEventItem key={item.key} ev={item.healing} />
                  ) : null
                )}
              </ul>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
