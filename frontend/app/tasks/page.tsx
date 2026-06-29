"use client";

import React, { useCallback, useMemo, useState } from "react";
import {
  ListTodo,
  CheckCircle2,
  XCircle,
  Loader2,
  Zap,
  Inbox,
  Copy,
  Check,
  Search,
  RotateCcw,
  X,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useSSEContext } from "@/components/providers/sse-provider";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

function statusIcon(eventType: string) {
  if (eventType.includes("completed"))
    return <CheckCircle2 className="size-3.5 text-emerald-400" />;
  if (eventType.includes("failed"))
    return <XCircle className="size-3.5 text-red-400" />;
  if (eventType.includes("processing"))
    return <Loader2 className="size-3.5 text-blue-400 animate-spin" />;
  return <Zap className="size-3.5 text-violet-400" />;
}

function statusBadgeClass(eventType: string): string {
  if (eventType.includes("completed"))
    return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
  if (eventType.includes("failed"))
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (eventType.includes("processing"))
    return "text-blue-400 border-blue-500/40 bg-blue-500/10";
  return "text-violet-400 border-violet-500/40 bg-violet-500/10";
}

function statusLabel(eventType: string): string {
  if (eventType.includes("completed")) return "completed";
  if (eventType.includes("failed")) return "failed";
  if (eventType.includes("processing")) return "processing";
  if (eventType.includes("created")) return "created";
  return eventType.replace("task.", "");
}

function formatDuration(ms: number | undefined): string {
  if (!ms || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function CopyCell({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const [expanded, setExpanded] = useState(false);

  function handleCopy(e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-xs text-zinc-400">
      <button
        onClick={() => setExpanded(!expanded)}
        className="hover:text-zinc-200 transition-colors cursor-pointer text-left"
        title="Click to expand"
      >
        {expanded ? text : `${text.slice(0, 12)}...`}
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

function ReplayModal({
  taskId,
  onClose,
}: {
  taskId: string;
  onClose: () => void;
}) {
  const [payload, setPayload] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleReplay = useCallback(async () => {
    setError(null);
    try {
      const body = payload.trim()
        ? JSON.stringify({ override_payload: payload.trim() })
        : undefined;
      const res = await fetch(`${API_URL}/api/tasks/${taskId}/replay`, {
        method: "POST",
        headers: {
          ...(TOKEN ? { "X-Internal-Token": TOKEN } : {}),
          ...(body ? { "Content-Type": "application/json" } : {}),
        },
        body,
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: "request failed" }));
        setError(data.error || `status ${res.status}`);
        return;
      }
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "network error");
    }
  }, [taskId, payload, onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-xl border border-zinc-800/80 bg-zinc-900 p-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-sm font-semibold text-zinc-100">Replay Task</h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300">
            <X className="size-4" />
          </button>
        </div>
        <p className="text-xs text-zinc-500 mb-3">
          Replaying <span className="font-mono text-zinc-400 break-all">{taskId}</span>
        </p>
        <label className="block text-[10px] font-medium text-zinc-500 uppercase tracking-wide mb-1.5">
          Override payload (optional)
        </label>
        <textarea
          value={payload}
          onChange={(e) => setPayload(e.target.value)}
          placeholder="Leave empty to use original input"
          className="w-full h-24 rounded-lg border border-zinc-800/80 bg-zinc-950 px-3 py-2 text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:ring-1 focus:ring-zinc-700 resize-none"
        />
        {error && (
          <p className="text-xs text-red-400 mt-2">{error}</p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            onClick={onClose}
            className="text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 bg-zinc-900/60 rounded-lg px-3 py-1.5 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleReplay}
            className="text-xs font-medium text-emerald-400 hover:text-emerald-300 border border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10 rounded-lg px-3 py-1.5 transition-colors"
          >
            Replay
          </button>
        </div>
      </div>
    </div>
  );
}

export default function TasksPage() {
  const { events } = useSSEContext();
  const [filter, setFilter] = useState("");
  const [replayTarget, setReplayTarget] = useState<string | null>(null);

  const latestByTask = useMemo(() => {
    const map = new Map<
      string,
      {
        task_id: string;
        trace_id: string;
        event_type: string;
        model?: string;
        duration_ms: number | null;
        prompt_tokens?: number;
        completion_tokens?: number;
        tokens_per_second?: number;
        timestamp: string;
      }
    >();

    for (const ev of events) {
      const existing = map.get(ev.task_id);
      if (
        !existing ||
        new Date(ev.timestamp).getTime() > new Date(existing.timestamp).getTime()
      ) {
        let durationMs: number | null = ev.inference_duration_ms ?? null;
        if (!durationMs && ev.processing_started_at && ev.completed_at) {
          durationMs =
            new Date(ev.completed_at).getTime() -
            new Date(ev.processing_started_at).getTime();
        }

        map.set(ev.task_id, {
          task_id: ev.task_id,
          trace_id: ev.trace_id,
          event_type: ev.event_type,
          model: ev.model,
          duration_ms: durationMs,
          prompt_tokens: ev.prompt_tokens,
          completion_tokens: ev.completion_tokens,
          tokens_per_second: ev.tokens_per_second,
          timestamp: ev.timestamp,
        });
      }
    }

    return Array.from(map.values()).sort(
      (a, b) =>
        new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    );
  }, [events]);

  const filtered = useMemo(() => {
    if (!filter.trim()) return latestByTask;
    const q = filter.toLowerCase();
    return latestByTask.filter(
      (t) =>
        t.task_id.toLowerCase().includes(q) ||
        t.trace_id.toLowerCase().includes(q) ||
        t.event_type.toLowerCase().includes(q) ||
        (t.model && t.model.toLowerCase().includes(q))
    );
  }, [latestByTask, filter]);

  return (
    <div className="px-6 py-5 lg:px-8">
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <ListTodo className="size-5 text-zinc-400" />
            Tasks
          </h1>
          <p className="text-sm text-zinc-500 mt-0.5">
            All tasks processed by the platform
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-zinc-500">
          <span className="tabular-nums">{latestByTask.length}</span> tasks
        </div>
      </header>

      {/* Search */}
      <div className="mb-4 relative max-w-sm">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-zinc-500" />
        <Input
          type="text"
          placeholder="Filter by ID, model, status..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="pl-8 bg-zinc-900/60 border-zinc-800/80 text-zinc-100 placeholder:text-zinc-500 text-xs h-8"
        />
      </div>

      {/* Table */}
      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Inbox className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">
            {events.length === 0
              ? "No tasks yet. Submit a task from the Overview."
              : "No tasks match your filter."}
          </p>
        </div>
      ) : (
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-zinc-800/60">
                <th className="py-2.5 pl-4 pr-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Task ID
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
                  Tokens
                </th>
                <th className="py-2.5 px-3 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  tok/s
                </th>
                <th className="py-2.5 px-3 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Created At
                </th>
                <th className="py-2.5 pl-3 pr-4 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Action
                </th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((task) => (
                <tr
                  key={task.task_id}
                  className="border-b border-zinc-800/40 last:border-b-0 hover:bg-zinc-800/30 transition-colors"
                >
                  <td className="py-2.5 pl-4 pr-3">
                    <CopyCell text={task.task_id} />
                  </td>
                  <td className="py-2.5 px-3">
                    <Badge
                      variant="outline"
                      className={`${statusBadgeClass(task.event_type)} text-[10px] uppercase font-semibold px-1.5 py-0 inline-flex items-center gap-1`}
                    >
                      {statusIcon(task.event_type)}
                      {statusLabel(task.event_type)}
                    </Badge>
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 font-mono">
                    {task.model ?? "—"}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 tabular-nums text-right">
                    {formatDuration(task.duration_ms ?? undefined)}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 tabular-nums text-right">
                    {task.prompt_tokens || task.completion_tokens
                      ? `${(task.prompt_tokens ?? 0) + (task.completion_tokens ?? 0)}`
                      : "—"}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-400 tabular-nums text-right">
                    {task.tokens_per_second
                      ? task.tokens_per_second.toFixed(1)
                      : "—"}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-500 tabular-nums text-right">
                    {new Date(task.timestamp).toLocaleTimeString()}
                  </td>
                  <td className="py-2.5 pl-3 pr-4 text-right">
                    {(task.event_type.includes("completed") ||
                      task.event_type.includes("failed")) && (
                      <button
                        onClick={() => setReplayTarget(task.task_id)}
                        className="inline-flex items-center gap-1 text-[10px] font-medium text-violet-400 hover:text-violet-300 border border-violet-500/30 bg-violet-500/5 hover:bg-violet-500/10 rounded px-2 py-0.5 transition-colors"
                      >
                        <RotateCcw className="size-2.5" />
                        Replay
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {replayTarget && (
        <ReplayModal
          taskId={replayTarget}
          onClose={() => setReplayTarget(null)}
        />
      )}
    </div>
  );
}
