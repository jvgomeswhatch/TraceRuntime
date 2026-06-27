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
  AlertCircle,
  Copy,
  Check,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import {
  timeAgo,
  displayEventType,
  severityBadgeClass,
  SeverityIcon,
} from "@/components/operations-summary";
import type { SSEEvent, HealingEvent } from "@/lib/types";

type TabKey = "all" | "tasks" | "operations" | "errors";

interface MergedItem {
  kind: "task" | "healing";
  timestamp: string;
  key: string;
  task?: SSEEvent;
  healing?: HealingEvent;
}

const TABS: { key: TabKey; label: string; icon: React.ReactNode }[] = [
  { key: "all", label: "All", icon: <Layers className="size-3.5" /> },
  { key: "tasks", label: "Tasks", icon: <Zap className="size-3.5" /> },
  {
    key: "operations",
    label: "Operations",
    icon: <Radio className="size-3.5" />,
  },
  {
    key: "errors",
    label: "Errors",
    icon: <AlertCircle className="size-3.5" />,
  },
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
    return <CheckCircle2 className="size-3.5 text-emerald-400" />;
  if (eventType.includes("failed"))
    return <XCircle className="size-3.5 text-red-400" />;
  if (eventType.includes("processing"))
    return <Loader2 className="size-3.5 text-blue-400 animate-spin" />;
  if (eventType.includes("created"))
    return <Zap className="size-3.5 text-violet-400" />;
  return <Radio className="size-3.5 text-zinc-400" />;
}

function truncateId(id: string, len = 10): string {
  if (id.length <= len) return id;
  return id.slice(0, len) + "...";
}

function CopyableId({ label, id }: { label?: string; id: string }) {
  const [copied, setCopied] = useState(false);
  const [expanded, setExpanded] = useState(false);

  function handleCopy(e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(id).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  return (
    <span className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-500">
      {label && <span className="text-zinc-600">{label}:</span>}
      <button
        onClick={(e) => { e.stopPropagation(); setExpanded(!expanded); }}
        className="hover:text-zinc-300 transition-colors cursor-pointer"
        title="Click to expand"
      >
        {expanded ? id : truncateId(id)}
      </button>
      <button
        onClick={handleCopy}
        className="hover:text-zinc-300 transition-colors shrink-0"
        title="Copy to clipboard"
      >
        {copied ? (
          <Check className="size-3 text-emerald-400" />
        ) : (
          <Copy className="size-3" />
        )}
      </button>
    </span>
  );
}

function ConnectionStatus({
  connected,
  reconnects,
}: {
  connected: boolean;
  reconnects: number;
}) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span
        className={`inline-block size-1.5 rounded-full ${
          connected
            ? "bg-emerald-400"
            : "bg-amber-400 animate-pulse"
        }`}
      />
      <span className={connected ? "text-zinc-400" : "text-amber-400"}>
        {connected ? "Connected" : "Reconnecting..."}
      </span>
      {reconnects > 0 && (
        <span className="text-zinc-500">({reconnects})</span>
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
    <div className="flex flex-col items-center justify-center py-8">
      <div className="flex items-center justify-center size-10 rounded-xl bg-zinc-800/50 border border-zinc-800/80 mb-2.5">
        <Icon className="size-4 text-zinc-500" />
      </div>
      <p className="text-xs font-medium text-zinc-400">{title}</p>
      <p className="text-[11px] mt-1 text-zinc-500">{subtitle}</p>
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
    <li className="group border-b border-zinc-800/40 last:border-b-0">
      <div className="flex items-center gap-2.5 px-4 py-2.5 hover:bg-zinc-800/30 transition-colors">
        <EventTypeIcon eventType={ev.event_type} />
        <Badge
          variant="outline"
          className={`${eventTypeColor(ev.event_type)} text-[11px] uppercase font-semibold px-1.5 py-0`}
        >
          {ev.event_type.replace("task.", "")}
        </Badge>

        {ev.model && (
          <span className="text-xs text-zinc-500 font-mono">{ev.model}</span>
        )}

        {ev.inference_duration_ms != null && ev.inference_duration_ms > 0 && (
          <span className="text-xs text-zinc-500 tabular-nums">
            {ev.inference_duration_ms < 1000
              ? `${ev.inference_duration_ms}ms`
              : `${(ev.inference_duration_ms / 1000).toFixed(1)}s`}
          </span>
        )}

        {ev.prompt_tokens != null && ev.prompt_tokens > 0 && (
          <span className="text-xs text-zinc-500 tabular-nums">
            {ev.prompt_tokens + (ev.completion_tokens ?? 0)} tok
          </span>
        )}

        {ev.tokens_per_second != null && ev.tokens_per_second > 0 && (
          <span className="text-xs text-zinc-600 tabular-nums">
            {ev.tokens_per_second.toFixed(1)} tok/s
          </span>
        )}

        <div className="ml-auto flex items-center gap-3">
          <CopyableId label="trace" id={ev.trace_id} />
          <span className="flex items-center gap-1 tabular-nums text-[11px] text-zinc-500">
            {new Date(ev.timestamp).toLocaleTimeString()}
          </span>
        </div>
      </div>

      {ev.error_reason && (
        <div className="mx-4 mb-2 rounded bg-red-500/10 border border-red-500/20 px-3 py-1.5 text-xs text-red-400">
          {ev.error_reason}
        </div>
      )}

      {ev.output && (
        <>
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex items-center gap-1.5 w-full px-4 py-1.5 text-[11px] text-zinc-500 hover:text-zinc-400 transition-colors"
          >
            {expanded ? (
              <ChevronDown className="size-3" />
            ) : (
              <ChevronRight className="size-3" />
            )}
            Output
          </button>
          {expanded && (
            <div className="px-4 pb-3">
              <pre className="rounded bg-zinc-950/60 border border-zinc-800/80 p-3 text-xs text-zinc-300 whitespace-pre-wrap break-words max-h-40 overflow-y-auto font-mono leading-relaxed">
                {ev.output}
              </pre>
            </div>
          )}
        </>
      )}
    </li>
  );
});

const HealingEventItem = React.memo(function HealingEventItem({
  ev,
}: {
  ev: HealingEvent;
}) {
  return (
    <li className="group border-b border-zinc-800/40 last:border-b-0">
      <div className="flex items-center gap-2.5 px-4 py-2.5 hover:bg-zinc-800/30 transition-colors">
        <SeverityIcon severity={ev.severity} />
        <Badge variant="outline" className={`${severityBadgeClass(ev.severity)} text-[11px] px-1.5 py-0`}>
          {displayEventType(ev.event_type)}
        </Badge>
        <Badge
          variant="outline"
          className={`text-[11px] px-1.5 py-0 uppercase font-semibold ${
            ev.status === "active"
              ? "text-red-400 border-red-500/40 bg-red-500/10"
              : "text-emerald-400 border-emerald-500/40 bg-emerald-500/10"
          }`}
        >
          {ev.status}
        </Badge>

        <div className="ml-auto flex items-center gap-3">
          {ev.worker_id && (
            <CopyableId id={ev.worker_id} />
          )}
          <span className="tabular-nums text-[11px] text-zinc-500">{timeAgo(ev.created_at)}</span>
        </div>
      </div>
    </li>
  );
});

export function EventFeed() {
  const {
    events,
    connected,
    reconnects,
    recentHealingEvents,
  } = useSSEContext();
  const [activeTab, setActiveTab] = useState<TabKey>("all");

  const errorEvents = useMemo(
    () => events.filter((ev) => ev.event_type.includes("failed") || ev.error_reason),
    [events]
  );

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
    all: events.length + recentHealingEvents.length,
    tasks: events.length,
    operations: recentHealingEvents.length,
    errors: errorEvents.length,
  };

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full flex flex-col">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            <Radio
              className={`size-4 ${connected ? "text-emerald-400" : "text-zinc-500"}`}
            />
            Event Stream
          </CardTitle>
          <ConnectionStatus connected={connected} reconnects={reconnects} />
        </div>

        <div className="flex gap-0.5 mt-3 p-0.5 rounded-lg bg-zinc-800/50 border border-zinc-800/80">
          {TABS.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-all ${
                activeTab === tab.key
                  ? "bg-zinc-700/80 text-zinc-100 shadow-sm shadow-black/20"
                  : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-700/30"
              }`}
            >
              {tab.icon}
              {tab.label}
              {tabCounts[tab.key] > 0 && (
                <span
                  className={`ml-0.5 inline-flex items-center justify-center min-w-[16px] h-[16px] rounded-full px-1 text-[11px] font-semibold tabular-nums ${
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

      <CardContent className="flex-1 min-h-0 overflow-hidden pt-0">
        <div className="h-full max-h-[540px] overflow-y-auto">
          {activeTab === "all" && (
            <>
              {mergedItems.length === 0 ? (
                <EmptyState
                  icon={Layers}
                  title="Waiting for events..."
                  subtitle="All events will be shown here"
                />
              ) : (
                <ul>
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

          {activeTab === "tasks" && (
            <>
              {events.length === 0 ? (
                <EmptyState
                  icon={Inbox}
                  title="No task events yet"
                  subtitle="Submit a task to see events appear here"
                />
              ) : (
                <ul>
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
                <ul>
                  {recentHealingEvents.map((ev) => (
                    <HealingEventItem key={ev.id} ev={ev} />
                  ))}
                </ul>
              )}
            </>
          )}

          {activeTab === "errors" && (
            <>
              {errorEvents.length === 0 ? (
                <EmptyState
                  icon={AlertCircle}
                  title="No errors"
                  subtitle="Failures and errors will appear here"
                />
              ) : (
                <ul>
                  {errorEvents.map((ev) => (
                    <TaskEventItem key={ev.event_id} ev={ev} />
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
