"use client";

import React, { useEffect, useState, useCallback } from "react";
import {
  Gauge,
  Activity,
  BarChart3,
  Clock,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Loader2,
  Inbox,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type {
  CapacityReport as CapacityReportType,
  CapacityLatencyBucket,
} from "@/lib/types";
import { formatDurationMs, formatDurationSec, formatThroughput, formatPercent, formatTimestamp } from "@/lib/format";

function statusBadgeClass(status: CapacityReportType["status"]): string {
  switch (status) {
    case "PASS":
      return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10 text-xs uppercase font-semibold";
    case "WARNING":
      return "text-amber-400 border-amber-500/40 bg-amber-500/10 text-xs uppercase font-semibold";
    case "FAIL":
      return "text-red-400 border-red-500/40 bg-red-500/10 text-xs uppercase font-semibold";
  }
}

interface LatencyRowProps {
  label: string;
  color: string;
  bucket: CapacityLatencyBucket;
}

const LatencyRow = React.memo(function LatencyRow({
  label,
  color,
  bucket,
}: LatencyRowProps) {
  return (
    <tr className="border-b border-zinc-800/40 last:border-b-0">
      <td className="py-2 pr-3 text-[11px] text-zinc-300 font-medium">
        <span className="flex items-center gap-1.5">
          <span className={`inline-block size-1.5 rounded-full ${color}`} />
          {label}
        </span>
      </td>
      <td className="py-2 px-2 text-[11px] text-zinc-400 tabular-nums text-right">
        {formatDurationMs(bucket.p50)}
      </td>
      <td className="py-2 px-2 text-[11px] text-amber-400 tabular-nums text-right font-medium">
        {formatDurationMs(bucket.p95)}
      </td>
      <td className="py-2 pl-2 text-[11px] text-red-400 tabular-nums text-right font-medium">
        {formatDurationMs(bucket.p99)}
      </td>
    </tr>
  );
});

const CapacityReportInner = React.memo(function CapacityReportInner() {
  const [report, setReport] = useState<CapacityReportType | null>(null);
  const [loading, setLoading] = useState(true);
  const [empty, setEmpty] = useState(false);

  const fetchReport = useCallback(async () => {
    try {
      const token = process.env.NEXT_PUBLIC_INTERNAL_TOKEN;
      const headers: HeadersInit = token ? { "X-Internal-Token": token } : {};
      const res = await fetch("http://localhost:8082/api/capacity/latest", { headers });
      if (res.status === 404) {
        setEmpty(true);
        setReport(null);
        return;
      }
      if (!res.ok) {
        setEmpty(true);
        setReport(null);
        return;
      }
      const data: CapacityReportType = await res.json();
      setReport(data);
      setEmpty(false);
    } catch {
      setEmpty(true);
      setReport(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchReport();
  }, [fetchReport]);

  if (loading) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            <Gauge className="size-4 text-blue-400" />
            Last Benchmark
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center gap-3 py-6 text-zinc-500 text-sm">
            <Loader2 className="size-4 animate-spin" />
            Loading...
          </div>
        </CardContent>
      </Card>
    );
  }

  if (empty || !report) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            <Gauge className="size-4 text-blue-400" />
            Last Benchmark
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center gap-2 py-6 text-zinc-500 text-sm">
            <Inbox className="size-6 text-zinc-600" />
            <p className="text-center text-xs">
              No benchmark data. Run{" "}
              <code className="text-zinc-400 bg-zinc-800/60 px-1.5 py-0.5 rounded text-[11px] font-mono">
                make loadtest
              </code>{" "}
              to generate a capacity report.
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  const errorRate =
    report.results.tasks_submitted > 0
      ? (report.results.tasks_failed / report.results.tasks_submitted) * 100
      : 0;

  const timestampStr = formatTimestamp(report.timestamp);

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 h-full">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
            <Gauge className="size-4 text-blue-400" />
            Last Benchmark
          </CardTitle>
          <Badge variant="outline" className={statusBadgeClass(report.status)}>
            {report.status}
          </Badge>
        </div>
        <p className="text-[11px] text-zinc-400 mt-1 font-medium">{timestampStr}</p>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Summary KPIs */}
        <div className="grid grid-cols-2 gap-2">
          <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-3">
            <div className="flex items-center gap-1.5 text-zinc-500 mb-1">
              <BarChart3 className="size-3" />
              <span className="text-[11px] font-medium">Tasks</span>
            </div>
            <div className="text-lg font-bold text-zinc-100 tabular-nums">
              {report.config.tasks}
            </div>
            <div className="text-[11px] text-zinc-500">submitted</div>
          </div>

          <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-3">
            <div className="flex items-center gap-1.5 text-zinc-500 mb-1">
              <Clock className="size-3" />
              <span className="text-[11px] font-medium">Duration</span>
            </div>
            <div className="text-lg font-bold text-zinc-100 tabular-nums">
              {formatDurationSec(report.results.duration_seconds)}
            </div>
            <div className="text-[11px] text-zinc-500">total runtime</div>
          </div>

          <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-3">
            <div className="flex items-center gap-1.5 text-zinc-500 mb-1">
              <Activity className="size-3" />
              <span className="text-[11px] font-medium">Throughput</span>
            </div>
            <div className="text-lg font-bold text-blue-400 tabular-nums">
              {formatThroughput(report.results.throughput_rps).value}
            </div>
            <div className="text-[11px] text-zinc-500">{formatThroughput(report.results.throughput_rps).unit}</div>
          </div>

          <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-3">
            <div className="flex items-center gap-1.5 text-zinc-500 mb-1">
              <AlertTriangle className="size-3" />
              <span className="text-[11px] font-medium">Error Rate</span>
            </div>
            <div
              className={`text-lg font-bold tabular-nums ${
                errorRate > 5
                  ? "text-red-400"
                  : errorRate > 0
                    ? "text-amber-400"
                    : "text-emerald-400"
              }`}
            >
              {formatPercent(errorRate)}
            </div>
            <div className="text-[11px] text-zinc-500">
              ({report.results.tasks_failed} failed)
            </div>
          </div>
        </div>

        {/* Latency Breakdown */}
        <div>
          <h3 className="text-[11px] font-medium text-zinc-500 mb-2 uppercase tracking-wide">
            Latency Breakdown
          </h3>
          <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 overflow-hidden">
            <table className="w-full">
              <thead>
                <tr className="border-b border-zinc-700/50">
                  <th className="py-1.5 pl-3 pr-2 text-left text-[11px] font-medium text-zinc-500 uppercase tracking-wide">
                    Phase
                  </th>
                  <th className="py-1.5 px-2 text-right text-[11px] font-medium text-zinc-500">
                    P50
                  </th>
                  <th className="py-1.5 px-2 text-right text-[11px] font-medium text-amber-500/70">
                    P95
                  </th>
                  <th className="py-1.5 pl-2 pr-3 text-right text-[11px] font-medium text-red-500/70">
                    P99
                  </th>
                </tr>
              </thead>
              <tbody>
                <LatencyRow
                  label="API Request"
                  color="bg-blue-400"
                  bucket={report.results.latency_ms.api_request}
                />
                <LatencyRow
                  label="Queue Wait"
                  color="bg-amber-400"
                  bucket={report.results.latency_ms.submit_to_processing}
                />
                <LatencyRow
                  label="Processing"
                  color="bg-emerald-400"
                  bucket={report.results.latency_ms.processing_duration}
                />
                <LatencyRow
                  label="End-to-End"
                  color="bg-violet-400"
                  bucket={report.results.latency_ms.end_to_end}
                />
              </tbody>
            </table>
          </div>
        </div>

        {/* Queue Behavior */}
        <div>
          <h3 className="text-[11px] font-medium text-zinc-500 mb-2 uppercase tracking-wide">
            Queue Behavior
          </h3>
          <div className="grid grid-cols-3 gap-2">
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-2.5 text-center">
              <div className="text-[11px] text-zinc-500 mb-0.5">
                Peak Backlog
              </div>
              <div className="text-sm font-bold text-zinc-100 tabular-nums">
                {report.queue_metrics.max_visible_messages}
              </div>
            </div>
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-2.5 text-center">
              <div className="text-[11px] text-zinc-500 mb-0.5">
                Peak In-Flight
              </div>
              <div className="text-sm font-bold text-zinc-100 tabular-nums">
                {report.queue_metrics.max_inflight_messages}
              </div>
            </div>
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/30 p-2.5 text-center">
              <div className="text-[11px] text-zinc-500 mb-0.5">Converged</div>
              <div className="flex items-center justify-center gap-1">
                {report.queue_metrics.backlog_converged ? (
                  <>
                    <CheckCircle2 className="size-3 text-emerald-400" />
                    <span className="text-sm font-semibold text-emerald-400">
                      Yes
                    </span>
                  </>
                ) : (
                  <>
                    <XCircle className="size-3 text-red-400" />
                    <span className="text-sm font-semibold text-red-400">
                      No
                    </span>
                  </>
                )}
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
});

export { CapacityReportInner as CapacityReport };
