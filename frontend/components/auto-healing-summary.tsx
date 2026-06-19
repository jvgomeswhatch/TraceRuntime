"use client";

import React from "react";
import { RefreshCw, Heart, RotateCcw } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";

export const AutoHealingSummary = React.memo(function AutoHealingSummary() {
  const { recentHealingEvents } = useSSEContext();

  const requeues = recentHealingEvents.filter(
    (e) => e.event_type === "task.stuck" || e.event_type.includes("requeue")
  ).length;

  const heartbeatLost = recentHealingEvents.filter(
    (e) =>
      e.event_type === "worker.stale" || e.event_type === "worker.down"
  ).length;

  const recoveries = recentHealingEvents.filter(
    (e) => e.status === "resolved"
  ).length;

  const stats = [
    {
      icon: <RefreshCw className="size-4 text-zinc-400" />,
      label: "Requeues",
      value: requeues,
    },
    {
      icon: <RotateCcw className="size-4 text-zinc-400" />,
      label: "Heartbeat Lost",
      value: heartbeatLost,
    },
    {
      icon: <Heart className="size-4 text-zinc-400" />,
      label: "Recoveries",
      value: recoveries,
    },
  ];

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between">
          <span className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            Auto-Healing
          </span>
          <span className="text-[11px] text-zinc-500 font-normal">
            Last 24h
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {stats.map((stat) => (
            <div
              key={stat.label}
              className="flex items-center gap-3 rounded-lg border border-zinc-800/80 bg-zinc-800/30 px-3.5 py-3"
            >
              {stat.icon}
              <div className="flex-1 min-w-0">
                <p className="text-[11px] text-zinc-500">{stat.label}</p>
              </div>
              <span className="text-xl font-bold text-zinc-100 tabular-nums">
                {stat.value}
              </span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
});
