"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  ArrowLeft,
  CheckCircle2,
  XCircle,
  Loader2,
  AlertTriangle,
  Clock,
  Layers,
  Cpu,
  GitBranch,
  Inbox,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { TraceDetailResponse, TraceSpan } from "@/lib/types";

const API_BASE = "http://localhost:8082";

const SERVICE_COLORS: Record<string, string> = {
  api: "bg-emerald-500",
  worker: "bg-teal-500",
  "ai-runtime": "bg-cyan-500",
  "otel-collector": "bg-zinc-500",
};

function serviceColor(name: string): string {
  const lower = name.toLowerCase();
  for (const [key, color] of Object.entries(SERVICE_COLORS)) {
    if (lower.includes(key)) return color;
  }
  const hash = Array.from(lower).reduce((acc, c) => acc + c.charCodeAt(0), 0);
  const palette = [
    "bg-emerald-400",
    "bg-teal-400",
    "bg-cyan-400",
    "bg-sky-400",
    "bg-green-400",
    "bg-emerald-600",
  ];
  return palette[hash % palette.length];
}

function statusAccentBar(status: string): string {
  switch (status) {
    case "completed":
      return "bg-emerald-500";
    case "failed":
      return "bg-red-500";
    case "processing":
      return "bg-emerald-500";
    default:
      return "bg-zinc-700";
  }
}

function spanStatusBadge(status: string) {
  switch (status) {
    case "ok":
      return (
        <Badge
          variant="outline"
          className="text-emerald-400 border-emerald-500/40 bg-emerald-500/10 text-[10px] uppercase font-semibold px-1.5 py-0 gap-1"
        >
          <CheckCircle2 className="size-3" />
          ok
        </Badge>
      );
    case "error":
      return (
        <Badge
          variant="outline"
          className="text-red-400 border-red-500/40 bg-red-500/10 text-[10px] uppercase font-semibold px-1.5 py-0 gap-1"
        >
          <XCircle className="size-3" />
          error
        </Badge>
      );
    default:
      return (
        <Badge
          variant="outline"
          className="text-zinc-400 border-zinc-600/40 bg-zinc-700/20 text-[10px] uppercase font-semibold px-1.5 py-0 gap-1"
        >
          unset
        </Badge>
      );
  }
}

function formatDuration(nanos: number): string {
  const ms = nanos / 1_000_000;
  if (ms < 1) return `${(nanos / 1000).toFixed(0)}µs`;
  if (ms < 1000) return `${ms.toFixed(1)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

interface SpanRow extends TraceSpan {
  depth: number;
  offsetPct: number;
  widthPct: number;
}

function buildSpanRows(spans: TraceSpan[]): SpanRow[] {
  if (spans.length === 0) return [];

  const minStart = Math.min(...spans.map((s) => s.start_time_unix_nano));
  const maxEnd = Math.max(
    ...spans.map((s) => s.start_time_unix_nano + s.duration_nano)
  );
  const totalRange = maxEnd - minStart || 1;

  const childrenMap = new Map<string, TraceSpan[]>();
  const roots: TraceSpan[] = [];
  const spanById = new Map<string, TraceSpan>();

  for (const s of spans) {
    spanById.set(s.span_id, s);
  }

  for (const s of spans) {
    if (!s.parent_span_id || !spanById.has(s.parent_span_id)) {
      roots.push(s);
    } else {
      const children = childrenMap.get(s.parent_span_id) ?? [];
      children.push(s);
      childrenMap.set(s.parent_span_id, children);
    }
  }

  roots.sort((a, b) => a.start_time_unix_nano - b.start_time_unix_nano);

  const rows: SpanRow[] = [];

  function walk(span: TraceSpan, depth: number) {
    const offsetPct =
      ((span.start_time_unix_nano - minStart) / totalRange) * 100;
    const widthPct = Math.max((span.duration_nano / totalRange) * 100, 0.5);

    rows.push({ ...span, depth, offsetPct, widthPct });

    const children = childrenMap.get(span.span_id) ?? [];
    children.sort((a, b) => a.start_time_unix_nano - b.start_time_unix_nano);
    for (const child of children) {
      walk(child, depth + 1);
    }
  }

  for (const root of roots) {
    walk(root, 0);
  }

  return rows;
}

export default function TraceDetailPage() {
  const params = useParams<{ traceId: string }>();
  const traceId = params.traceId;

  const [data, setData] = useState<TraceDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchTrace = useCallback(async (signal?: AbortSignal) => {
    const token = process.env.NEXT_PUBLIC_INTERNAL_TOKEN;
    const headers: HeadersInit = token ? { "X-Internal-Token": token } : {};
    const res = await fetch(`${API_BASE}/api/traces/${traceId}`, { signal, headers });
    if (res.status === 404) throw new Error("Trace not found");
    if (!res.ok) throw new Error(`API returned ${res.status}`);
    return res.json();
  }, [traceId]);

  useEffect(() => {
    if (!traceId) return;

    const controller = new AbortController();
    setLoading(true);
    setError(null);

    fetchTrace(controller.signal)
      .then((json) => {
        if (!controller.signal.aborted) {
          setData(json);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          setError(err.message);
          setLoading(false);
        }
      });

    return () => controller.abort();
  }, [traceId, fetchTrace]);

  useEffect(() => {
    const isTerminal = data?.task?.status === "completed" || data?.task?.status === "failed";
    if (!data || isTerminal) {
      if (pollRef.current) clearInterval(pollRef.current);
      return;
    }

    pollRef.current = setInterval(() => {
      fetchTrace().then(setData).catch(() => {});
    }, 5_000);

    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [data, fetchTrace]);

  const spans = data?.spans ?? [];
  const services = data?.services ?? [];

  const spanRows = useMemo(() => {
    if (spans.length === 0) return [];
    return buildSpanRows(spans);
  }, [spans]);

  const selectedSpan = selectedSpanId
    ? spanRows.find((s) => s.span_id === selectedSpanId) ?? null
    : null;

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <Loader2 className="size-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="px-6 py-5 lg:px-8">
        <Link
          href="/traces"
          className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 transition-colors mb-5"
        >
          <ArrowLeft className="size-3.5" />
          Back to traces
        </Link>
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <AlertTriangle className="size-8 text-red-500/60 mb-3" />
          <p className="text-sm text-red-400">{error}</p>
          <p className="text-xs text-zinc-600 mt-1">Trace ID: {traceId}</p>
        </div>
      </div>
    );
  }

  if (!data) return null;

  const task = data.task;
  const taskStatus = task?.status ?? "unknown";
  const totalDurationNano = (() => {
    if (spanRows.length > 0) {
      return (
        Math.max(...spans.map((s) => s.start_time_unix_nano + s.duration_nano)) -
        Math.min(...spans.map((s) => s.start_time_unix_nano))
      );
    }
    if (task?.processing_started_at && task?.completed_at) {
      return (
        (new Date(task.completed_at).getTime() -
          new Date(task.processing_started_at).getTime()) *
        1_000_000
      );
    }
    return null;
  })();

  return (
    <div className="px-6 py-5 lg:px-8">
      {/* Header */}
      <header className="mb-4 flex items-center justify-between">
        <div>
          <Link
            href="/traces"
            className="flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300 transition-colors mb-1.5"
          >
            <ArrowLeft className="size-3" />
            Back to traces
          </Link>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <GitBranch className="size-5 text-zinc-400" />
            Trace Detail
          </h1>
          <p className="text-xs text-zinc-500 mt-0.5">
            Distributed trace across the pipeline
          </p>
        </div>
        {task && (
          <div
            className={`flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium ${
              taskStatus === "completed"
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
                : taskStatus === "failed"
                  ? "border-red-500/30 bg-red-500/10 text-red-400"
                  : "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
            }`}
          >
            {taskStatus === "completed" && <CheckCircle2 className="size-3.5" />}
            {taskStatus === "failed" && <XCircle className="size-3.5" />}
            {taskStatus === "processing" && (
              <Loader2 className="size-3.5 animate-spin" />
            )}
            {taskStatus.charAt(0).toUpperCase() + taskStatus.slice(1)}
          </div>
        )}
      </header>

      {/* Tempo warning */}
      {!data.tempo_available && (
        <div className="mb-3 flex items-center gap-2 text-xs text-amber-400 bg-amber-500/10 border border-amber-500/20 rounded-xl px-3 py-2">
          <AlertTriangle className="size-3.5 shrink-0" />
          Tempo unavailable — showing database data only. Spans may have expired (1h retention).
        </div>
      )}

      {/* Stats row */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-3">
        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="flex items-center gap-2 text-zinc-400 mb-3">
            <GitBranch className="size-4" />
            <span className="text-sm font-medium">Trace ID</span>
          </div>
          <p className="font-mono text-xs text-zinc-300 break-all">{data.trace_id}</p>
        </div>

        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="flex items-center gap-2 text-zinc-400 mb-3">
            <Clock className="size-4" />
            <span className="text-sm font-medium">Duration</span>
          </div>
          <span className={`text-2xl font-bold tabular-nums tracking-tight ${totalDurationNano ? "text-emerald-400" : "text-zinc-500"}`}>
            {totalDurationNano ? formatDuration(totalDurationNano) : "—"}
          </span>
        </div>

        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="flex items-center gap-2 text-zinc-400 mb-3">
            <Layers className="size-4" />
            <span className="text-sm font-medium">Spans</span>
          </div>
          <div className="flex items-baseline gap-1.5">
            <span className={`text-2xl font-bold tabular-nums ${spans.length > 0 ? "text-emerald-400" : "text-zinc-500"}`}>
              {spans.length}
            </span>
            <span className="text-zinc-500 text-xs">
              · {services.length} service{services.length !== 1 ? "s" : ""}
            </span>
          </div>
        </div>

        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="flex items-center gap-2 text-zinc-400 mb-3">
            <Cpu className="size-4" />
            <span className="text-sm font-medium">Model</span>
          </div>
          <span className="text-lg font-semibold text-zinc-100 font-mono">
            {task?.model ?? "—"}
          </span>
          {task && (task.prompt_tokens > 0 || task.completion_tokens > 0) && (
            <p className="text-[11px] text-zinc-500 mt-0.5 tabular-nums">
              {task.prompt_tokens + task.completion_tokens} tokens · {task.tokens_per_second > 0 ? `${task.tokens_per_second.toFixed(1)} tok/s` : "—"}
            </p>
          )}
        </div>
      </div>

      {/* Service Legend */}
      {services.length > 0 && (
        <div className="flex flex-wrap items-center gap-4 mb-3 px-1">
          {services.map((svc) => (
            <div key={svc} className="flex items-center gap-1.5">
              <span className={`size-2.5 rounded-full ${serviceColor(svc)}`} />
              <span className="text-xs text-zinc-400">{svc}</span>
            </div>
          ))}
        </div>
      )}

      {/* Waterfall */}
      {spanRows.length > 0 ? (
        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden mb-3">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="px-5 py-3 border-b border-zinc-800/60 flex items-center gap-2">
            <Layers className="size-4 text-zinc-400" />
            <span className="text-sm font-medium text-zinc-400">Span Waterfall</span>
          </div>
          <div className="divide-y divide-zinc-800/40">
            {spanRows.map((row) => {
              const isSelected = selectedSpanId === row.span_id;
              return (
                <button
                  key={row.span_id}
                  onClick={() => setSelectedSpanId(isSelected ? null : row.span_id)}
                  className={`w-full text-left grid grid-cols-[220px_1fr_80px] items-center px-5 py-2 hover:bg-zinc-800/30 transition-colors ${
                    isSelected ? "bg-emerald-500/5 border-l-2 border-l-emerald-500" : "border-l-2 border-l-transparent"
                  }`}
                >
                  <div className="flex items-center gap-1.5 truncate text-xs">
                    <span className={`shrink-0 size-2 rounded-full ${serviceColor(row.service_name)}`} />
                    <span className={`truncate ${isSelected ? "text-emerald-400 font-medium" : "text-zinc-300"}`}>
                      {row.operation_name}
                    </span>
                  </div>

                  <div className="h-4 relative">
                    <div
                      className={`absolute top-0.5 h-3 rounded-sm ${serviceColor(row.service_name)} ${
                        row.status === "error" ? "opacity-90" : "opacity-50"
                      }`}
                      style={{
                        left: `${row.offsetPct}%`,
                        width: `${row.widthPct}%`,
                        minWidth: "3px",
                      }}
                    />
                    {row.status === "error" && (
                      <div
                        className="absolute top-0 h-4 border-l-2 border-red-500"
                        style={{ left: `${row.offsetPct}%` }}
                      />
                    )}
                  </div>

                  <span className="text-xs text-zinc-500 font-mono tabular-nums text-right">
                    {formatDuration(row.duration_nano)}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      ) : (
        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden mb-3">
          <div className="absolute top-0 left-0 right-0 h-[2px] bg-zinc-700" />
          <div className="flex flex-col items-center justify-center py-12 text-zinc-500">
            <Inbox className="size-8 text-zinc-600 mb-3" />
            <p className="text-sm">No spans available</p>
            {!data.tempo_available && (
              <p className="text-xs text-zinc-600 mt-1">Tempo is unreachable — traces may have expired</p>
            )}
          </div>
        </div>
      )}

      {/* Selected Span Detail */}
      {selectedSpan && (
        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 overflow-hidden mb-3">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${serviceColor(selectedSpan.service_name)}`} />
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <span className={`size-2.5 rounded-full ${serviceColor(selectedSpan.service_name)}`} />
              <span className="text-sm font-medium text-zinc-200">{selectedSpan.operation_name}</span>
            </div>
            {spanStatusBadge(selectedSpan.status)}
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
            <div>
              <span className="text-zinc-500">Service</span>
              <p className="text-zinc-300 mt-0.5">{selectedSpan.service_name}</p>
            </div>
            <div>
              <span className="text-zinc-500">Duration</span>
              <p className="text-emerald-400 font-mono mt-0.5 tabular-nums">{formatDuration(selectedSpan.duration_nano)}</p>
            </div>
            <div>
              <span className="text-zinc-500">Span ID</span>
              <p className="text-zinc-400 font-mono mt-0.5 text-[10px]">{selectedSpan.span_id}</p>
            </div>
            <div>
              <span className="text-zinc-500">Parent</span>
              <p className="text-zinc-400 font-mono mt-0.5 text-[10px]">{selectedSpan.parent_span_id || "— (root)"}</p>
            </div>
          </div>

          {selectedSpan.attributes && Object.keys(selectedSpan.attributes).length > 0 && (
            <div className="mt-3 pt-3 border-t border-zinc-800/40">
              <p className="text-[10px] text-zinc-500 uppercase tracking-wide mb-2">Attributes</p>
              <div className="bg-zinc-950/50 rounded-lg p-3 space-y-1">
                {Object.entries(selectedSpan.attributes).map(([key, value]) => (
                  <div key={key} className="flex items-start gap-3 text-[11px]">
                    <span className="text-zinc-500 shrink-0 font-mono">{key}</span>
                    <span className="text-zinc-300 font-mono break-all">{value}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Task Details */}
      {task && (
        <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden">
          <div className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(taskStatus)}`} />
          <div className="px-5 py-3 border-b border-zinc-800/60 flex items-center gap-2">
            <Cpu className="size-4 text-zinc-400" />
            <span className="text-sm font-medium text-zinc-400">Task Details</span>
          </div>

          <div className="p-5">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
              <div className="rounded-lg bg-zinc-800/30 p-3">
                <span className="text-[11px] text-zinc-500 block mb-1">Task ID</span>
                <p className="text-zinc-300 font-mono text-[10px] break-all leading-relaxed">{task.id}</p>
              </div>
              <div className="rounded-lg bg-zinc-800/30 p-3">
                <span className="text-[11px] text-zinc-500 block mb-1">Created</span>
                <p className="text-sm font-medium text-zinc-200 tabular-nums">{new Date(task.created_at).toLocaleString()}</p>
              </div>
              {task.processing_started_at && (
                <div className="rounded-lg bg-zinc-800/30 p-3">
                  <span className="text-[11px] text-zinc-500 block mb-1">Processing Started</span>
                  <p className="text-sm font-medium text-zinc-200 tabular-nums">{new Date(task.processing_started_at).toLocaleTimeString()}</p>
                </div>
              )}
              {task.completed_at && (
                <div className="rounded-lg bg-zinc-800/30 p-3">
                  <span className="text-[11px] text-zinc-500 block mb-1">Completed</span>
                  <p className="text-sm font-medium text-zinc-200 tabular-nums">{new Date(task.completed_at).toLocaleTimeString()}</p>
                </div>
              )}
            </div>

            {(task.prompt_tokens > 0 || task.completion_tokens > 0) && (
              <div className="grid grid-cols-3 gap-4 mt-4">
                <div className="rounded-lg bg-zinc-800/30 p-3">
                  <span className="text-[11px] text-zinc-500 block mb-1">Prompt Tokens</span>
                  <p className="text-lg font-bold text-zinc-100 font-mono tabular-nums">{task.prompt_tokens.toLocaleString()}</p>
                </div>
                <div className="rounded-lg bg-zinc-800/30 p-3">
                  <span className="text-[11px] text-zinc-500 block mb-1">Completion Tokens</span>
                  <p className="text-lg font-bold text-zinc-100 font-mono tabular-nums">{task.completion_tokens.toLocaleString()}</p>
                </div>
                <div className="rounded-lg bg-zinc-800/30 p-3">
                  <span className="text-[11px] text-zinc-500 block mb-1">Speed</span>
                  <p className="text-lg font-bold text-emerald-400 font-mono tabular-nums">
                    {task.tokens_per_second > 0 ? `${task.tokens_per_second.toFixed(1)}` : "—"}
                    {task.tokens_per_second > 0 && <span className="text-xs text-zinc-500 ml-1">tok/s</span>}
                  </p>
                </div>
              </div>
            )}

            {task.error_message && (
              <div className="mt-4 rounded-lg bg-red-500/5 border border-red-500/20 p-3">
                <span className="text-[10px] text-red-400 uppercase tracking-wide font-semibold">Error</span>
                <p className="text-xs text-red-300 font-mono mt-1.5 leading-relaxed">{task.error_message}</p>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
