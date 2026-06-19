"use client";

import { CheckCircle2 } from "lucide-react";
import { useSSEContext } from "@/components/providers/sse-provider";

export function OverviewHeader() {
  const { connected, workers, activeHealingEvents } = useSSEContext();

  const healthyCount = workers.filter((w) => w.status === "healthy").length;
  const allNominal =
    connected &&
    workers.length > 0 &&
    healthyCount === workers.length &&
    activeHealingEvents.length === 0;

  return (
    <header className="mb-4 flex items-center justify-between">
      <div>
        <h1 className="text-xl font-semibold text-zinc-100 tracking-tight">
          Overview
        </h1>
        <p className="text-xs text-zinc-500 mt-0.5">
          Real-time operational overview of your AI Runtime platform
        </p>
      </div>
      <div
        className={`flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium ${
          allNominal
            ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
            : connected
              ? "border-amber-500/30 bg-amber-500/10 text-amber-400"
              : "border-zinc-700 bg-zinc-800/50 text-zinc-500"
        }`}
      >
        <CheckCircle2 className="size-3.5" />
        {allNominal
          ? "All systems nominal"
          : connected
            ? `${activeHealingEvents.length} active incident${activeHealingEvents.length !== 1 ? "s" : ""}`
            : "Disconnected"}
      </div>
    </header>
  );
}
