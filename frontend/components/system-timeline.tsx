"use client";

import React, { useMemo } from "react";
import { BarChart3 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import { useSSEContext } from "@/components/providers/sse-provider";

interface TimelinePoint {
  time: string;
  events: number;
}

function buildTimeline(events: { timestamp: string }[]): TimelinePoint[] {
  const now = Date.now();
  const bucketMs = 60_000;
  const totalBuckets = 15;
  const points: TimelinePoint[] = [];

  for (let i = totalBuckets - 1; i >= 0; i--) {
    const bucketStart = now - i * bucketMs;
    const d = new Date(bucketStart);
    const label = `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
    points.push({ time: label, events: 0 });
  }

  for (const ev of events) {
    const age = now - new Date(ev.timestamp).getTime();
    const bucketIndex = Math.floor(age / bucketMs);
    if (bucketIndex >= 0 && bucketIndex < totalBuckets) {
      const pointIndex = totalBuckets - 1 - bucketIndex;
      points[pointIndex].events++;
    }
  }

  return points;
}

export const SystemTimeline = React.memo(function SystemTimeline() {
  const { events } = useSSEContext();

  const data = useMemo(() => buildTimeline(events), [events]);

  const hasData = data.some((d) => d.events > 0);

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            <BarChart3 className="size-4 text-blue-400" />
            System Health Timeline
          </CardTitle>
          <span className="text-[11px] text-zinc-500">
            Event throughput · last 15 min
          </span>
        </div>
      </CardHeader>
      <CardContent>
        {!hasData ? (
          <div className="flex items-center justify-center h-[160px] text-xs text-zinc-500">
            No event data available — submit tasks to populate the timeline
          </div>
        ) : (
          <div className="h-[180px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={data}>
                <defs>
                  <linearGradient id="timeline-grad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#34d399" stopOpacity={0.2} />
                    <stop offset="100%" stopColor="#34d399" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid
                  strokeDasharray="3 3"
                  stroke="rgba(255,255,255,0.05)"
                  vertical={false}
                />
                <XAxis
                  dataKey="time"
                  tick={{ fontSize: 10, fill: "#71717a" }}
                  axisLine={{ stroke: "rgba(255,255,255,0.1)" }}
                  tickLine={false}
                  interval="preserveStartEnd"
                />
                <YAxis
                  tick={{ fontSize: 10, fill: "#71717a" }}
                  axisLine={false}
                  tickLine={false}
                  width={24}
                  allowDecimals={false}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "#18181b",
                    border: "1px solid rgba(255,255,255,0.1)",
                    borderRadius: "8px",
                    fontSize: "11px",
                    color: "#e4e4e7",
                  }}
                  labelStyle={{ color: "#a1a1aa", fontSize: "10px" }}
                  formatter={(value) => [`${value} events`, "Throughput"]}
                />
                <Area
                  type="monotone"
                  dataKey="events"
                  name="Events / min"
                  stroke="#34d399"
                  strokeWidth={2}
                  fill="url(#timeline-grad)"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
});
