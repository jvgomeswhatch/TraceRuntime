"use client";

import { useState } from "react";
import { Send, Terminal, Sparkles, Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { CreateTaskResponse } from "@/lib/types";

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  return (
    <button
      onClick={handleCopy}
      className="text-zinc-400 hover:text-zinc-200 transition-colors"
      title="Copy to clipboard"
    >
      {copied ? (
        <Check className="size-3.5 text-emerald-400" />
      ) : (
        <Copy className="size-3.5" />
      )}
    </button>
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
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-emerald-500/30 border-t-2">
      <CardHeader>
        <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
          <Send className="size-5 text-emerald-400" />
          Create Task
        </CardTitle>
        <p className="text-sm text-zinc-400 mt-1">
          Submit a prompt to the AI runtime. The task will be queued, processed
          by a worker, and results streamed back via SSE.
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="relative">
            <Terminal className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-zinc-500" />
            <Input
              type="text"
              placeholder="Enter task input..."
              value={input}
              onChange={(e) => setInput(e.target.value)}
              disabled={loading}
              className="pl-10 bg-zinc-800/60 border-zinc-700/80 text-zinc-100 placeholder:text-zinc-500 text-base h-11 focus-visible:border-emerald-500/50 focus-visible:ring-emerald-500/20"
            />
          </div>
          <Button
            type="submit"
            disabled={loading || !input.trim()}
            className="w-full bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium h-10 transition-all duration-200 disabled:opacity-40"
          >
            {loading ? (
              <>
                <Sparkles className="size-4 animate-pulse" />
                Processing...
              </>
            ) : (
              <>
                <Send className="size-4" />
                Submit Task
              </>
            )}
          </Button>
        </form>

        {error && (
          <div className="rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {last && (
          <div className="space-y-3 border-t border-zinc-800/80 pt-4">
            <p className="text-xs font-medium text-zinc-400 uppercase tracking-wider">
              Last submitted
            </p>
            <div className="space-y-2 rounded-lg bg-zinc-800/40 border border-zinc-800/80 p-3">
              <div className="flex items-center justify-between text-sm">
                <span className="text-zinc-400">task_id</span>
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs text-zinc-300 truncate max-w-[180px]">
                    {last.task_id}
                  </span>
                  <CopyButton text={last.task_id} />
                </div>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-zinc-400">trace_id</span>
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs text-zinc-300 truncate max-w-[180px]">
                    {last.trace_id}
                  </span>
                  <CopyButton text={last.trace_id} />
                </div>
              </div>
            </div>
          </div>
        )}

        {!last && !error && (
          <div className="rounded-lg border border-dashed border-zinc-800/80 bg-zinc-800/20 p-4">
            <div className="flex items-start gap-3">
              <Sparkles className="size-4 text-zinc-400 mt-0.5 shrink-0" />
              <div className="text-xs text-zinc-400 space-y-1">
                <p>Tasks flow through the full pipeline:</p>
                <p className="text-zinc-500">
                  API &rarr; Queue &rarr; Worker &rarr; AI Runtime &rarr; SSE
                </p>
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
