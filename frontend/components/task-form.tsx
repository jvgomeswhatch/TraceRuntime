"use client";

import { useState } from "react";
import { Send, Terminal, Sparkles, Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { CreateTaskResponse } from "@/lib/types";

function ExpandableId({ text }: { text: string }) {
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
    <div className="flex items-center gap-1.5">
      <button
        onClick={() => setExpanded(!expanded)}
        className="font-mono text-[11px] text-zinc-300 hover:text-zinc-100 transition-colors cursor-pointer text-right break-all"
        title="Click to expand"
      >
        {expanded ? text : `${text.slice(0, 12)}...`}
      </button>
      <button
        onClick={handleCopy}
        className="text-zinc-400 hover:text-zinc-200 transition-colors shrink-0"
        title="Copy to clipboard"
      >
        {copied ? (
          <Check className="size-3 text-emerald-400" />
        ) : (
          <Copy className="size-3" />
        )}
      </button>
    </div>
  );
}

export function TaskForm() {
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<CreateTaskResponse | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!input.trim()) return;

    setLoading(true);
    setError(null);

    try {
      const res = await fetch("http://localhost:8082/tasks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ input: input.trim() }),
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        setError((body as { error?: string }).error ?? `HTTP ${res.status}`);
        return;
      }

      const data: CreateTaskResponse = await res.json();
      setLast(data);
      setInput("");
    } catch {
      setError("Failed to reach API — is the Go server running on :8082?");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full flex flex-col">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
          <Send className="size-4 text-emerald-400" />
          Create Task
        </CardTitle>
        <p className="text-[11px] text-zinc-500 mt-1">
          Submit a new prompt to the AI runtime
        </p>
      </CardHeader>
      <CardContent className="space-y-4 flex-1">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="relative">
            <Terminal className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-zinc-500" />
            <Input
              type="text"
              placeholder="Enter task input..."
              value={input}
              onChange={(e) => setInput(e.target.value)}
              disabled={loading}
              className="pl-8 bg-zinc-800/60 border-zinc-700/80 text-zinc-100 placeholder:text-zinc-500 text-xs h-9 focus-visible:border-emerald-500/50 focus-visible:ring-emerald-500/20"
            />
          </div>
          <Button
            type="submit"
            disabled={loading || !input.trim()}
            className="w-full bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-medium h-8 transition-all disabled:opacity-40"
          >
            {loading ? (
              <>
                <Sparkles className="size-3.5 animate-pulse" />
                Processing...
              </>
            ) : (
              "+ New Task"
            )}
          </Button>
        </form>

        {error && (
          <div className="rounded bg-red-500/10 border border-red-500/20 px-3 py-2 text-[11px] text-red-400">
            {error}
          </div>
        )}

        {last && (
          <div className="space-y-2 border-t border-zinc-800/80 pt-3">
            <p className="text-[11px] font-medium text-zinc-500 uppercase tracking-wider">
              Last submitted
            </p>
            <div className="space-y-1.5 rounded bg-zinc-800/40 border border-zinc-800/80 p-2.5">
              <div className="flex items-center justify-between text-xs">
                <span className="text-zinc-500">task_id</span>
                <ExpandableId text={last.task_id} />
              </div>
              <div className="flex items-center justify-between text-xs">
                <span className="text-zinc-500">trace_id</span>
                <ExpandableId text={last.trace_id} />
              </div>
            </div>
          </div>
        )}

        {!last && !error && (
          <div className="rounded-lg border border-dashed border-zinc-800/80 bg-zinc-800/20 p-3">
            <div className="flex items-start gap-2.5">
              <Sparkles className="size-3.5 text-zinc-500 mt-0.5 shrink-0" />
              <div className="text-[11px] text-zinc-500 space-y-1">
                <p className="text-zinc-400">Pipeline flow:</p>
                <p className="font-mono text-zinc-500">
                  API &rarr; SQS &rarr; Worker &rarr; AI Runtime &rarr; SSE
                </p>
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
