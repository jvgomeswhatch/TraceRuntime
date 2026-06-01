"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { CreateTaskResponse } from "@/lib/types";

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
      setError("Failed to reach API — is the Go server running on :8080?");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
          Create Task
        </CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="flex gap-2">
          <Input
            type="text"
            placeholder="Enter task input…"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            disabled={loading}
            className="flex-1 bg-zinc-800 border-zinc-600 text-zinc-100 placeholder:text-zinc-500 font-mono text-sm"
          />
          <Button
            type="submit"
            disabled={loading || !input.trim()}
            className="bg-emerald-700 hover:bg-emerald-600 text-white font-mono text-sm"
          >
            {loading ? "Sending…" : "Submit"}
          </Button>
        </form>

        {error && (
          <p className="mt-2 text-xs font-mono text-red-400">{error}</p>
        )}

        {last && (
          <div className="mt-3 text-xs font-mono text-zinc-400 space-y-1">
            <div>
              <span className="text-zinc-500">task_id </span>
              {last.task_id}
            </div>
            <div>
              <span className="text-zinc-500">trace_id </span>
              {last.trace_id}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
