"use client";

import React, { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
  CheckCircle2,
  Clock,
  AlertTriangle,
  Inbox,
  TrendingUp,
  TrendingDown,
  Minus,
} from "lucide-react";
import {
  ResponsiveContainer,
  AreaChart,
  Area,
} from "recharts";
import type { RuntimeMetrics } from "@/lib/types";
import { useSSEContext } from "@/components/providers/sse-provider";

interface KpiData {
  id: string;
  label: string;
  value: string;
  unit: string;
  trend: "up" | "down" | "flat";
  trendLabel: string;
  sparkData: { v: number }[];
  color: string;
  trendColor: string;
  icon: React.ReactNode;
}

function buildThroughputSpark(events: { timestamp: string }[]): { v: number }[] {
  if (events.length === 0) return [];

  const now = Date.now();
  const bucketMs = 60_000;
  const totalBuckets = 15;
  const buckets = new Array<number>(totalBuckets).fill(0);

  for (const ev of events) {
    const age = now - new Date(ev.timestamp).getTime();
    const idx = Math.floor(age / bucketMs);
    if (idx >= 0 && idx < totalBuckets) {
      buckets[totalBuckets - 1 - idx]++;
    }
  }

  const hasAny = buckets.some((b) => b > 0);
  if (!hasAny) return [];

  return buckets.map((count) => ({ v: count }));
}

export const KpiCards = React.memo(function KpiCards() {
  const [metrics, setMetrics] = useState<RuntimeMetrics | null>(null);
  const { events } = useSSEContext();
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchMetrics = useCallback(async () => {
    try {
      const token = process.env.NEXT_PUBLIC_INTERNAL_TOKEN;
      const headers: HeadersInit = token ? { "X-Internal-Token": token } : {};
      const res = await fetch("http://localhost:8082/api/metrics/runtime", { headers });
      if (!res.ok) return;
      const data: RuntimeMetrics = await res.json();
      setMetrics(data);
    } catch {
      // runtime metrics unavailable
    }
  }, []);

  useEffect(() => {
    fetchMetrics();
    intervalRef.current = setInterval(fetchMetrics, 10_000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchMetrics]);

  const throughputSpark = useMemo(() => buildThroughputSpark(events), [events]);

  const kpis = useMemo<KpiData[]>(() => {
    const p95 = metrics ? metrics.p95_latency_ms / 1000 : 0;
    const avgLatency = metrics ? metrics.avg_latency_ms / 1000 : 0;
    const errorRate = metrics?.error_rate ?? 0;
    const queueDepth = metrics?.queue_depth ?? 0;
    const totalAll = metrics ? (metrics.total_completed ?? 0) + (metrics.total_failed ?? 0) : 0;

    return [
      {
        id: "tasks-processed",
        label: "Tasks Processed",
        value: metrics ? String(totalAll) : "—",
        unit: "",
        trend: totalAll > 0 ? "up" : "flat",
        trendLabel: metrics
          ? `${metrics.total_completed ?? 0} completed · ${metrics.total_failed ?? 0} failed`
          : "no data",
        sparkData: throughputSpark,
        color: "#34d399",
        trendColor: totalAll > 0 ? "text-emerald-400" : "text-zinc-500",
        icon: <CheckCircle2 className="size-3.5" />,
      },
      {
        id: "p95-latency",
        label: "P95 Latency",
        value: metrics ? p95.toFixed(2) : "—",
        unit: "s",
        trend: p95 > 3 ? "up" : p95 > 0 ? "down" : "flat",
        trendLabel: metrics ? `avg: ${avgLatency.toFixed(1)}s` : "no data",
        sparkData: [],
        color: "#fbbf24",
        trendColor:
          p95 > 3 ? "text-amber-400" : p95 > 0 ? "text-emerald-400" : "text-zinc-500",
        icon: <Clock className="size-3.5" />,
      },
      {
        id: "error-rate",
        label: "Error Rate",
        value: metrics ? `${errorRate.toFixed(1)}%` : "—",
        unit: "",
        trend: errorRate > 5 ? "up" : errorRate > 0 ? "flat" : "down",
        trendLabel: metrics ? `${metrics.failed} failed` : "no data",
        sparkData: [],
        color: errorRate > 5 ? "#f87171" : "#34d399",
        trendColor:
          errorRate > 5
            ? "text-red-400"
            : errorRate > 0
              ? "text-amber-400"
              : "text-emerald-400",
        icon: <AlertTriangle className="size-3.5" />,
      },
      {
        id: "queue-depth",
        label: "Queue Depth",
        value: metrics ? String(queueDepth) : "—",
        unit: "",
        trend: queueDepth > 10 ? "up" : queueDepth > 0 ? "down" : "flat",
        trendLabel: metrics ? `${metrics.completed} completed` : "no data",
        sparkData: [],
        color: "#a78bfa",
        trendColor:
          queueDepth > 10 ? "text-amber-400" : queueDepth > 0 ? "text-emerald-400" : "text-zinc-500",
        icon: <Inbox className="size-3.5" />,
      },
    ];
  }, [metrics, throughputSpark]);

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      {kpis.map((kpi) => (
        <div
          key={kpi.id}
          className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 flex flex-col justify-between"
        >
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-1.5 text-zinc-400">
              {kpi.icon}
              <span className="text-xs font-medium">{kpi.label}</span>
            </div>
            {kpi.trend === "up" ? (
              <TrendingUp className={`size-3.5 ${kpi.trendColor}`} />
            ) : kpi.trend === "down" ? (
              <TrendingDown className={`size-3.5 ${kpi.trendColor}`} />
            ) : (
              <Minus className="size-3.5 text-zinc-500" />
            )}
          </div>

          <div className="flex items-end justify-between">
            <div>
              <span className="text-2xl font-bold text-zinc-100 tabular-nums tracking-tight">
                {kpi.value}
              </span>
              {kpi.unit && (
                <span className="text-sm text-zinc-500 ml-1">{kpi.unit}</span>
              )}
              <p className={`text-[11px] mt-0.5 ${kpi.trendColor}`}>
                {kpi.trendLabel}
              </p>
            </div>

            {kpi.sparkData.length > 0 && (
              <div className="w-20 h-8">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={kpi.sparkData}>
                    <defs>
                      <linearGradient
                        id={`grad-${kpi.id}`}
                        x1="0"
                        y1="0"
                        x2="0"
                        y2="1"
                      >
                        <stop
                          offset="0%"
                          stopColor={kpi.color}
                          stopOpacity={0.3}
                        />
                        <stop
                          offset="100%"
                          stopColor={kpi.color}
                          stopOpacity={0}
                        />
                      </linearGradient>
                    </defs>
                    <Area
                      type="monotone"
                      dataKey="v"
                      stroke={kpi.color}
                      strokeWidth={1.5}
                      fill={`url(#grad-${kpi.id})`}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
});
