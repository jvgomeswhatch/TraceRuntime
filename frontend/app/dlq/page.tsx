"use client";

import React, { useEffect, useState } from "react";
import { Inbox, RefreshCw, Trash2, RotateCcw, AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

interface DLQMessage {
  message_id: string;
  receipt_handle: string;
  task_id: string;
  trace_id: string;
  payload: string;
  receive_count: string;
  task_status: string;
  error_message: string;
  message_body: string;
  message_attributes: Record<string, string>;
}

const apiHeaders: Record<string, string> = TOKEN
  ? { "X-Internal-Token": TOKEN }
  : {};

async function fetchMessagesApi(): Promise<{ messages: DLQMessage[]; approximate_count: number }> {
  const res = await fetch(`${API_URL}/api/dlq/messages`, { headers: apiHeaders });
  const data = await res.json();
  return { messages: data.messages || [], approximate_count: data.approximate_count || 0 };
}

async function fetchStatsApi(): Promise<number> {
  const res = await fetch(`${API_URL}/api/dlq/stats`, { headers: apiHeaders });
  const data = await res.json();
  return data.approximate_messages || 0;
}

export default function DLQPage() {
  const [messages, setMessages] = useState<DLQMessage[]>([]);
  const [approxCount, setApproxCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [purgeDisabled, setPurgeDisabled] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function doFetch() {
      setLoading(true);
      try {
        const result = await fetchMessagesApi();
        if (!cancelled) {
          setMessages(result.messages);
          setApproxCount(result.approximate_count);
        }
      } catch {
        /* ignore */
      }
      if (!cancelled) setLoading(false);
    }

    doFetch();

    const interval = setInterval(async () => {
      try {
        const [count, result] = await Promise.all([fetchStatsApi(), fetchMessagesApi()]);
        if (!cancelled) {
          setApproxCount(count);
          if (result.messages.length > 0) {
            setMessages(result.messages);
          }
        }
      } catch {
        /* ignore */
      }
    }, 10000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const refreshMessages = async () => {
    setLoading(true);
    try {
      const result = await fetchMessagesApi();
      setMessages(result.messages);
      setApproxCount(result.approximate_count);
    } catch {
      /* ignore */
    }
    setLoading(false);
  };

  const retryMessage = async (msg: DLQMessage) => {
    try {
      await fetch(`${API_URL}/api/dlq/messages/${msg.message_id}/retry`, {
        method: "POST",
        headers: {
          ...apiHeaders,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          receipt_handle: msg.receipt_handle,
          message_body: msg.message_body,
          message_attributes: msg.message_attributes,
        }),
      });
      setMessages((prev) => prev.filter((m) => m.message_id !== msg.message_id));
    } catch {
      /* ignore */
    }
  };

  const deleteMessage = async (msg: DLQMessage) => {
    if (!confirm("Delete this message permanently? This cannot be undone.")) return;
    try {
      await fetch(`${API_URL}/api/dlq/messages/${msg.message_id}`, {
        method: "DELETE",
        headers: {
          ...apiHeaders,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          receipt_handle: msg.receipt_handle,
          task_id: msg.task_id,
          trace_id: msg.trace_id,
          payload_preview: msg.payload || "",
        }),
      });
      setMessages((prev) => prev.filter((m) => m.message_id !== msg.message_id));
    } catch {
      /* ignore */
    }
  };

  const purgeAll = async () => {
    if (!confirm("Delete all messages from DLQ? This cannot be undone.")) return;
    try {
      await fetch(`${API_URL}/api/dlq/purge`, {
        method: "POST",
        headers: apiHeaders,
      });
      setMessages([]);
      setPurgeDisabled(true);
      setTimeout(() => setPurgeDisabled(false), 60000);
    } catch {
      /* ignore */
    }
  };

  return (
    <div className="px-6 py-5 lg:px-8">
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <Inbox className="size-5 text-zinc-400" />
            DLQ Explorer
          </h1>
          <p className="text-sm text-zinc-500 mt-0.5">
            Dead letter queue --- failed messages awaiting action
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={purgeAll}
            disabled={purgeDisabled}
            className="inline-flex items-center gap-1.5 text-xs font-medium text-red-400 hover:text-red-300 border border-red-500/30 bg-red-500/5 hover:bg-red-500/10 rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50"
          >
            <Trash2 className="size-3" />
            {purgeDisabled ? "Wait 60s" : "Delete All"}
          </button>
          <button
            onClick={refreshMessages}
            disabled={loading}
            className="inline-flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 bg-zinc-900/60 rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50"
          >
            <RefreshCw className={`size-3 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </button>
        </div>
      </header>

      {/* Stats */}
      <div className="grid grid-cols-2 gap-3 mb-5">
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Messages in DLQ</p>
          <p className="text-lg font-semibold text-red-400 tabular-nums">{approxCount}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
          <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Loaded</p>
          <p className="text-lg font-semibold text-zinc-100 tabular-nums">{messages.length}</p>
        </div>
      </div>

      {/* Messages */}
      {messages.length === 0 && !loading ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Inbox className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">No messages in DLQ</p>
        </div>
      ) : (
        <div className="space-y-3">
          {messages.map((msg) => (
            <div
              key={msg.message_id}
              className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 space-y-3"
            >
              <div className="flex items-start justify-between">
                <div className="space-y-1.5 min-w-0 flex-1">
                  <div className="flex items-center gap-2.5">
                    <span className="text-xs font-mono text-zinc-300 break-all">
                      {msg.task_id}
                    </span>
                    <Badge
                      variant="outline"
                      className="text-red-400 border-red-500/40 bg-red-500/10 text-[10px] uppercase font-semibold px-1.5 py-0"
                    >
                      {msg.task_status || "failed"}
                    </Badge>
                    <Badge
                      variant="outline"
                      className="text-zinc-400 border-zinc-700 bg-zinc-800/50 text-[10px] font-medium px-1.5 py-0"
                    >
                      {msg.receive_count}x received
                    </Badge>
                  </div>
                  <p className="text-[10px] text-zinc-500 font-mono break-all">
                    trace: {msg.trace_id}
                  </p>
                  {msg.error_message && (
                    <p className="text-xs text-red-400 flex items-center gap-1.5">
                      <AlertTriangle className="size-3 shrink-0" />
                      {msg.error_message}
                    </p>
                  )}
                </div>
              </div>

              {msg.payload && (
                <div className="rounded-lg bg-zinc-950 border border-zinc-800/60 px-3 py-2">
                  <p className="text-xs text-zinc-400 font-mono break-all">
                    {msg.payload}
                  </p>
                </div>
              )}

              <div className="flex gap-2">
                <button
                  onClick={() => retryMessage(msg)}
                  className="inline-flex items-center gap-1 text-[10px] font-medium text-emerald-400 hover:text-emerald-300 border border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10 rounded px-2 py-0.5 transition-colors"
                >
                  <RotateCcw className="size-2.5" />
                  Retry
                </button>
                <button
                  onClick={() => deleteMessage(msg)}
                  className="inline-flex items-center gap-1 text-[10px] font-medium text-red-400 hover:text-red-300 border border-red-500/30 bg-red-500/5 hover:bg-red-500/10 rounded px-2 py-0.5 transition-colors"
                >
                  <Trash2 className="size-2.5" />
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
