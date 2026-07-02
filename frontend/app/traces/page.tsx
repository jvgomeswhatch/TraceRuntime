"use client";

import React, { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
  GitBranch,
  Inbox,
  Search,
  CheckCircle2,
  XCircle,
  Loader2,
  Zap,
  ChevronRight,
  Copy,
  Check,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useSSEContext } from "@/components/providers/sse-provider";
import type { SSEEvent } from "@/lib/types";

interface TraceGroup {
  trace_id: string;
  events: SSEEvent[];
  latestStatus: string;
  totalDuration: number | null;
  model: string | null;
  startTime: string;
}

function traceStatusIcon(status: string) {
  if (status.includes("completed"))
    return <CheckCircle2 className="size-3.5 text-emerald-400" />;
  if (status.includes("failed"))
    return <XCircle className="size-3.5 text-red-400" />;
  if (status.includes("processing"))
    return <Loader2 className="size-3.5 text-blue-400 animate-spin" />;
  return <Zap className="size-3.5 text-violet-400" />;
}

function traceStatusColor(status: string): string {
  if (status.includes("completed"))
    return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
  if (status.includes("failed"))
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (status.includes("processing"))
    return "text-blue-400 border-blue-500/40 bg-blue-500/10";
  return "text-violet-400 border-violet-500/40 bg-violet-500/10";
}

function CopyableId({ id }: { id: string }) {
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
    <span className="inline-flex items-center gap-1.5 font-mono text-xs text-zinc-400">
      <button
        onClick={(e) => { e.stopPropagation(); setExpanded(!expanded); }}
        className="hover:text-zinc-200 transition-colors cursor-pointer text-left"
        title="Click to expand"
      >
        {expanded ? id : `${id.slice(0, 12)}...`}
      </button>
      <button
        onClick={handleCopy}
        className="hover:text-zinc-200 transition-colors shrink-0"
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

export default function TracesPage() {
  const { events } = useSSEContext();
  const router = useRouter();
  const [filter, setFilter] = useState("");

  const traceGroups = useMemo<TraceGroup[]>(() => {
    const map = new Map<string, SSEEvent[]>();

    for (const ev of events) {
      const group = map.get(ev.trace_id) ?? [];
      group.push(ev);
      map.set(ev.trace_id, group);
    }

    return Array.from(map.entries())
      .map(([trace_id, traceEvents]) => {
        const sorted = traceEvents.sort(
          (a, b) =>
            new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
        );
        const latest = sorted[sorted.length - 1];
        const earliest = sorted[0];

        const completedEv = sorted.find((e) =>
          e.event_type.includes("completed")
        );
        const createdEv = sorted.find((e) =>
          e.event_type.includes("created")
        );

        const evWithTimestamps = sorted.find((e) => e.processing_started_at && e.completed_at);
        const totalDuration =
          evWithTimestamps?.processing_started_at && evWithTimestamps?.completed_at
            ? new Date(evWithTimestamps.completed_at).getTime() -
              new Date(evWithTimestamps.processing_started_at).getTime()
            : createdEv && completedEv
              ? new Date(completedEv.timestamp).getTime() -
                new Date(createdEv.timestamp).getTime()
              : null;

        return {
          trace_id,
          events: sorted,
          latestStatus: latest.event_type,
          totalDuration,
          model: latest.model ?? null,
          startTime: earliest.timestamp,
        };
      })
      .sort(
        (a, b) =>
          new Date(b.startTime).getTime() -
          new Date(a.startTime).getTime()
      );
  }, [events]);

  const filtered = useMemo(() => {
    if (!filter.trim()) return traceGroups;
    const q = filter.toLowerCase();
    return traceGroups.filter(
      (t) =>
        t.trace_id.toLowerCase().includes(q) ||
        t.latestStatus.toLowerCase().includes(q) ||
        (t.model && t.model.toLowerCase().includes(q))
    );
  }, [traceGroups, filter]);

  return (
    <div className="px-6 py-5 lg:px-8">
      <header className="mb-5">
        <div>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <GitBranch className="size-5 text-zinc-400" />
            Traces
          </h1>
          <p className="text-sm text-zinc-500 mt-0.5">
            Distributed traces across the pipeline
          </p>
        </div>
      </header>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-5">
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Total Traces</p>
          <p className="text-lg font-semibold text-zinc-100 tabular-nums">{traceGroups.length}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Completed</p>
          <p className="text-lg font-semibold text-emerald-400 tabular-nums">
            {traceGroups.filter((t) => t.latestStatus.includes("completed")).length}
          </p>
        </div>
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Failed</p>
          <p className="text-lg font-semibold text-red-400 tabular-nums">
            {traceGroups.filter((t) => t.latestStatus.includes("failed")).length}
          </p>
        </div>
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">In Progress</p>
          <p className="text-lg font-semibold text-blue-400 tabular-nums">
            {traceGroups.filter((t) => t.latestStatus.includes("processing")).length}
          </p>
        </div>
      </div>

      {/* Search */}
      <div className="mb-4 relative max-w-sm">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-zinc-500" />
        <Input
          type="text"
          placeholder="Filter by trace ID, model, status..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="pl-8 bg-zinc-900/60 border-zinc-800/80 text-zinc-100 placeholder:text-zinc-500 text-xs h-8"
        />
      </div>

      {/* Trace List */}
      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Inbox className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">
            {events.length === 0
              ? "No traces yet. Submit a task from the Overview."
              : "No traces match your filter."}
          </p>
        </div>
      ) : (
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-zinc-800/60">
                <th className="py-2.5 pl-4 pr-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Trace ID
                </th>
                <th className="py-2.5 px-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Status
                </th>
                <th className="py-2.5 px-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Model
                </th>
                <th className="py-2.5 px-3 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Duration
                </th>
                <th className="py-2.5 px-3 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Spans
                </th>
                <th className="py-2.5 pl-3 pr-4 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Time
                </th>
                <th className="py-2.5 pr-4 w-8" />
              </tr>
            </thead>
            <tbody>
              {filtered.map((trace) => (
                <tr
                  key={trace.trace_id}
                  onClick={() => router.push(`/traces/${trace.trace_id}`)}
                  className="border-b border-zinc-800/40 last:border-b-0 hover:bg-zinc-800/30 transition-colors cursor-pointer group"
                >
                  <td className="py-2.5 pl-4 pr-3">
                    <CopyableId id={trace.trace_id} />
                  </td>
                  <td className="py-2.5 px-3">
                    <Badge
                      variant="outline"
                      className={`${traceStatusColor(trace.latestStatus)} text-[10px] uppercase font-semibold px-1.5 py-0 inline-flex items-center gap-1`}
                    >
                      {traceStatusIcon(trace.latestStatus)}
                      {trace.latestStatus.replace("task.", "")}
                    </Badge>
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 font-mono">
                    {trace.model ?? "—"}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 tabular-nums text-right">
                    {trace.totalDuration !== null
                      ? trace.totalDuration < 1000
                        ? `${trace.totalDuration}ms`
                        : `${(trace.totalDuration / 1000).toFixed(2)}s`
                      : "—"}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-500 tabular-nums text-right">
                    {trace.events.length}
                  </td>
                  <td className="py-2.5 pl-3 pr-4 text-xs text-zinc-500 tabular-nums text-right">
                    {new Date(trace.startTime).toLocaleTimeString()}
                  </td>
                  <td className="py-2.5 pr-4">
                    <Link
                      href={`/traces/${trace.trace_id}`}
                      className="inline-flex items-center text-zinc-600 group-hover:text-zinc-400 transition-colors"
                    >
                      <ChevronRight className="size-3.5" />
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
