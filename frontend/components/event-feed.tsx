"use client";

import React, { useState, useMemo } from "react";
import {
  Zap,
  Radio,
  Layers,
  Clock,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  Loader2,
  XCircle,
  Inbox,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import {
  timeAgo,
  severityBadgeClass,
  SeverityIcon,
} from "@/components/operations-summary";
import type { SSEEvent, HealingEvent } from "@/lib/types";

type TabKey = "tasks" | "operations" | "all";

interface MergedItem {
  kind: "task" | "healing";
  timestamp: string;
  key: string;
  task?: SSEEvent;
  healing?: HealingEvent;
}

const TABS: { key: TabKey; label: string; icon: React.ReactNode }[] = [
  { key: "tasks", label: "Tasks", icon: <Zap className="size-4" /> },
  { key: "operations", label: "Operations", icon: <Radio className="size-4" /> },
  { key: "all", label: "All", icon: <Layers className="size-4" /> },
];

function eventTypeColor(eventType: string): string {
  if (eventType.includes("completed"))
    return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
  if (eventType.includes("failed"))
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (eventType.includes("processing"))
    return "text-blue-400 border-blue-500/40 bg-blue-500/10";
  if (eventType.includes("created"))
    return "text-violet-400 border-violet-500/40 bg-violet-500/10";
  return "text-zinc-400 border-zinc-500/40 bg-zinc-500/10";
}

function EventTypeIcon({ eventType }: { eventType: string }) {
  if (eventType.includes("completed"))
    return <CheckCircle2 className="size-4 text-emerald-400" />;
  if (eventType.includes("failed"))
    return <XCircle className="size-4 text-red-400" />;
  if (eventType.includes("processing"))
    return <Loader2 className="size-4 text-blue-400 animate-spin" />;
  if (eventType.includes("created"))
    return <Zap className="size-4 text-violet-400" />;
  return <Radio className="size-4 text-zinc-400" />;
}

function truncateId(id: string, len = 12): string {
  if (id.length <= len) return id;
  return id.slice(0, len) + "...";
}

/** Connection status indicator with contextual messaging */
function ConnectionStatus({
  connected,
  connectedAt,
  reconnects,
  lastEventAt,
}: {
  connected: boolean;
  connectedAt: Date | null;
  reconnects: number;
  lastEventAt: Date | null;
}) {
  const isInitialConnect = !connected && reconnects === 0 && !lastEventAt;
  const isReconnecting = !connected && (reconnects > 0 || lastEventAt !== null);

  let dotClass: string;
  let label: string;
  let labelClass: string;

  if (connected) {
    dotClass = "bg-emerald-400 shadow-sm shadow-emerald-400/50";
    label = "Connected";
    labelClass = "text-zinc-400";
  } else if (isInitialConnect) {
    dotClass = "bg-amber-400/70 shadow-sm shadow-amber-400/30 animate-pulse";
    label = "Connecting...";
    labelClass = "text-zinc-500";
  } else if (isReconnecting) {
    dotClass = "bg-amber-400 shadow-sm shadow-amber-400/50 animate-pulse";
    label = "Reconnecting...";
    labelClass = "text-amber-400/80";
  } else {
    dotClass = "bg-red-400 shadow-sm shadow-red-400/50";
    label = "Disconnected";
    labelClass = "text-red-400";
  }

  return (
    <div className="flex items-center gap-4 text-sm">
      <div className="flex items-center gap-2">
        <span className={`inline-block size-2 rounded-full ${dotClass}`} />
        <span className={labelClass}>{label}</span>
      </div>
      {reconnects > 0 && (
        <span className="text-zinc-400">
          {reconnects} reconnect{reconnects !== 1 ? "s" : ""}
        </span>
      )}
      {connectedAt && connected && (
        <span className="text-zinc-400">
          {connectedAt.toLocaleTimeString()}
        </span>
      )}
    </div>
  );
}

function EmptyState({
  icon: Icon,
  title,
  subtitle,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <div className="flex items-center justify-center size-16 rounded-2xl bg-zinc-800/50 border border-zinc-800/80 mb-4">
        <Icon className="size-7 text-zinc-500" />
      </div>
      <p className="text-sm font-medium text-zinc-400">{title}</p>
      <p className="text-xs mt-1.5 text-zinc-500">{subtitle}</p>
    </div>
  );
}

const TaskEventItem = React.memo(function TaskEventItem({
  ev,
}: {
  ev: SSEEvent;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <li className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 overflow-hidden hover:border-zinc-700/80 transition-all duration-150 hover:bg-zinc-800/40">
      <div className="flex items-center gap-3 px-4 py-3">
        <EventTypeIcon eventType={ev.event_type} />
        <Badge
          variant="outline"
          className={`${eventTypeColor(ev.event_type)} text-xs uppercase font-semibold`}
        >
          {ev.event_type.replace("task.", "")}
        </Badge>

        {ev.model && (
          <span className="text-sm text-zinc-500">{ev.model}</span>
        )}

        {ev.inference_duration_ms != null && ev.inference_duration_ms > 0 && (
          <span className="text-sm text-zinc-500 tabular-nums">
            {ev.inference_duration_ms < 1000
              ? `${ev.inference_duration_ms}ms`
              : `${(ev.inference_duration_ms / 1000).toFixed(1)}s`}
          </span>
        )}

        <div className="ml-auto flex items-center gap-1.5 text-sm text-zinc-400">
          <Clock className="size-3.5" />
          {timeAgo(ev.timestamp)}
        </div>
      </div>

      <div className="flex items-center gap-4 px-4 pb-2.5 text-sm text-zinc-400">
        <span className="font-mono text-xs" title={ev.task_id}>
          task:{truncateId(ev.task_id)}
        </span>
        <span className="font-mono text-xs" title={ev.trace_id}>
          trace:{truncateId(ev.trace_id)}
        </span>
        {ev.execution_status && <span>{ev.execution_status}</span>}
        {ev.validation_status && <span>validation: {ev.validation_status}</span>}
      </div>

      {ev.error_reason && (
        <div className="mx-4 mb-3 rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-2.5 text-sm text-red-400">
          {ev.error_reason}
        </div>
      )}

      {ev.output && (
        <div className="border-t border-zinc-800/80">
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm text-zinc-500 hover:text-zinc-400 transition-colors"
          >
            {expanded ? (
              <ChevronDown className="size-4" />
            ) : (
              <ChevronRight className="size-4" />
            )}
            Output
          </button>
          {expanded && (
            <div className="px-4 pb-4">
              <pre className="rounded-lg bg-zinc-950/60 border border-zinc-800/80 p-4 text-sm text-zinc-300 whitespace-pre-wrap break-words max-h-60 overflow-y-auto font-mono leading-relaxed">
                {ev.output}
              </pre>
            </div>
          )}
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
  const [showDetails, setShowDetails] = useState(false);

  const statusBadgeClass =
    ev.status === "active"
      ? "text-amber-400 border-amber-500/40 bg-amber-500/10"
      : "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";

  const detailEntries = Object.entries(ev.details).slice(0, 5);

  return (
    <li className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 overflow-hidden hover:border-zinc-700/80 transition-all duration-150 hover:bg-zinc-800/40">
      <div className="flex items-center gap-3 px-4 py-3">
        <SeverityIcon severity={ev.severity} />
        <Badge variant="outline" className={severityBadgeClass(ev.severity)}>
          {ev.event_type}
        </Badge>
        <Badge
          variant="outline"
          className={`${statusBadgeClass} text-xs uppercase font-semibold`}
        >
          {ev.status}
        </Badge>

        <div className="ml-auto flex items-center gap-3">
          {ev.worker_id && (
            <span
              className="text-sm text-zinc-400 font-mono"
              title={ev.worker_id}
            >
              {truncateId(ev.worker_id)}
            </span>
          )}
          <div className="flex items-center gap-1.5 text-sm text-zinc-400">
            <Clock className="size-3.5" />
            {timeAgo(ev.created_at)}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-4 px-4 pb-2.5 text-sm text-zinc-500">
        <span>source: {ev.source}</span>
        {ev.resolved_at && (
          <span className="text-emerald-500">
            resolved {timeAgo(ev.resolved_at)}
          </span>
        )}
      </div>

      {detailEntries.length > 0 && (
        <div className="border-t border-zinc-800/80">
          <button
            onClick={() => setShowDetails(!showDetails)}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm text-zinc-500 hover:text-zinc-400 transition-colors"
          >
            {showDetails ? (
              <ChevronDown className="size-4" />
            ) : (
              <ChevronRight className="size-4" />
            )}
            Details ({detailEntries.length})
          </button>
          {showDetails && (
            <div className="px-4 pb-4">
              <div className="rounded-lg bg-zinc-950/60 border border-zinc-800/80 p-4 space-y-1.5">
                {detailEntries.map(([k, v]) => (
                  <div key={k} className="flex items-baseline gap-3 text-sm">
                    <span className="text-zinc-500 shrink-0">{k}</span>
                    <span className="text-zinc-400 truncate font-mono text-xs">
                      {String(v)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </li>
  );
});

export function EventFeed() {
  const {
    events,
    connected,
    connectedAt,
    reconnects,
    lastEventAt,
    recentHealingEvents,
  } = useSSEContext();
  const [activeTab, setActiveTab] = useState<TabKey>("tasks");

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
      (a, b) =>
        new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    );
  }, [activeTab, events, recentHealingEvents]);

  const tabCounts: Record<TabKey, number> = {
    tasks: events.length,
    operations: recentHealingEvents.length,
    all: events.length + recentHealingEvents.length,
  };

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-emerald-500/30 border-t-2">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Radio
              className={`size-5 ${connected ? "text-emerald-400" : "text-zinc-500"}`}
            />
            Event Stream
          </CardTitle>
          <ConnectionStatus
            connected={connected}
            connectedAt={connectedAt}
            reconnects={reconnects}
            lastEventAt={lastEventAt}
          />
        </div>

        <div className="flex gap-1 mt-4 p-1 rounded-xl bg-zinc-800/50 border border-zinc-800/80">
          {TABS.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-lg transition-all duration-150 ${
                activeTab === tab.key
                  ? "bg-zinc-700/80 text-zinc-100 shadow-sm shadow-black/20"
                  : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-700/30"
              }`}
            >
              {tab.icon}
              {tab.label}
              {tabCounts[tab.key] > 0 && (
                <span
                  className={`ml-1 inline-flex items-center justify-center min-w-[20px] h-[20px] rounded-full px-1.5 text-xs font-semibold tabular-nums ${
                    activeTab === tab.key
                      ? "bg-zinc-600/80 text-zinc-200"
                      : "bg-zinc-700/50 text-zinc-500"
                  }`}
                >
                  {tabCounts[tab.key]}
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
              <EmptyState
                icon={Inbox}
                title="No task events yet"
                subtitle="Submit a task to see events appear here"
              />
            ) : (
              <ul className="space-y-2 max-h-[600px] overflow-y-auto pr-1">
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
              <EmptyState
                icon={Radio}
                title="No healing events yet"
                subtitle="Operational events will appear here"
              />
            ) : (
              <ul className="space-y-2 max-h-[600px] overflow-y-auto pr-1">
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
              <EmptyState
                icon={Layers}
                title="Waiting for events..."
                subtitle="All events will be shown here"
              />
            ) : (
              <ul className="space-y-2 max-h-[600px] overflow-y-auto pr-1">
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
