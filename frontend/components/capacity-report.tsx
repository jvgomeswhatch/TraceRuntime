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
import type { CapacityReport as CapacityReportType, CapacityLatencyBucket } from "@/lib/types";

function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return `${m}m ${s}s`;
}

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

function StatusIcon({ status }: { status: CapacityReportType["status"] }) {
  switch (status) {
    case "PASS":
      return <CheckCircle2 className="size-4 text-emerald-400" />;
    case "WARNING":
      return <AlertTriangle className="size-4 text-amber-400" />;
    case "FAIL":
      return <XCircle className="size-4 text-red-400" />;
  }
}

interface LatencyRowProps {
  label: string;
  bucket: CapacityLatencyBucket;
}

const LatencyRow = React.memo(function LatencyRow({ label, bucket }: LatencyRowProps) {
  return (
    <tr className="border-b border-zinc-800/60 last:border-b-0">
      <td className="py-2.5 pr-4 text-sm text-zinc-300 font-medium">{label}</td>
      <td className="py-2.5 px-3 text-sm text-zinc-300 tabular-nums text-right">{formatMs(bucket.p50)}</td>
      <td className="py-2.5 px-3 text-sm text-amber-400 tabular-nums text-right font-medium">{formatMs(bucket.p95)}</td>
      <td className="py-2.5 px-3 text-sm text-red-400 tabular-nums text-right font-medium">{formatMs(bucket.p99)}</td>
      <td className="py-2.5 px-3 text-sm text-zinc-400 tabular-nums text-right">{formatMs(bucket.min)}</td>
      <td className="py-2.5 pl-3 text-sm text-zinc-400 tabular-nums text-right">{formatMs(bucket.max)}</td>
    </tr>
  );
});

const CapacityReportInner = React.memo(function CapacityReportInner() {
  const [report, setReport] = useState<CapacityReportType | null>(null);
  const [loading, setLoading] = useState(true);
  const [empty, setEmpty] = useState(false);

  const fetchReport = useCallback(async () => {
    try {
      const res = await fetch("http://localhost:8082/api/capacity/latest");
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
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
        <CardHeader>
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center gap-3 py-10 text-zinc-500 text-base">
            <Loader2 className="size-5 animate-spin" />
            Loading capacity report...
          </div>
        </CardContent>
      </Card>
    );
  }

  if (empty || !report) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
        <CardHeader>
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center gap-3 py-10 text-zinc-500 text-base">
            <Inbox className="size-8 text-zinc-600" />
            <p>No capacity results available. Run <code className="text-zinc-400 bg-zinc-800/60 px-2 py-0.5 rounded text-sm font-mono">make loadtest</code> to generate baselines.</p>
          </div>
        </CardContent>
      </Card>
    );
  }

  const errorRate =
    report.results.tasks_submitted > 0
      ? (report.results.tasks_failed / report.results.tasks_submitted) * 100
      : 0;

  const ts = new Date(report.timestamp);
  const timestampStr = ts.toLocaleString();

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
          <div className="flex items-center gap-3">
            <span className="text-sm text-zinc-500">{timestampStr}</span>
            <Badge
              variant="outline"
              className={statusBadgeClass(report.status)}
            >
              <StatusIcon status={report.status} />
              {report.status}
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {/* Config cards row */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <BarChart3 className="size-4" />
              <span className="text-sm font-medium">Tasks</span>
            </div>
            <div className="text-2xl font-bold text-blue-400 tabular-nums">
              {report.config.tasks}
            </div>
            <div className="text-xs text-zinc-500 mt-1">
              {report.config.rate} req/s target
            </div>
          </div>

          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <Activity className="size-4" />
              <span className="text-sm font-medium">Throughput</span>
            </div>
            <div className="text-2xl font-bold text-blue-400 tabular-nums">
              {report.results.throughput_rps.toFixed(1)}
            </div>
            <div className="text-xs text-zinc-500 mt-1">req/s actual</div>
          </div>

          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <Clock className="size-4" />
              <span className="text-sm font-medium">Duration</span>
            </div>
            <div className="text-2xl font-bold text-zinc-100 tabular-nums">
              {formatDuration(report.results.duration_seconds)}
            </div>
            <div className="text-xs text-zinc-500 mt-1">test runtime</div>
          </div>

          <div className="group rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4 transition-all duration-200 hover:border-zinc-700/80 hover:bg-zinc-800/50 hover:shadow-md hover:shadow-black/10">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <AlertTriangle className="size-4" />
              <span className="text-sm font-medium">Error Rate</span>
            </div>
            <div
              className={`text-2xl font-bold tabular-nums ${
                errorRate > 5
                  ? "text-red-400"
                  : errorRate > 0
                    ? "text-amber-400"
                    : "text-emerald-400"
              }`}
            >
              {errorRate.toFixed(1)}%
            </div>
            <div className="text-xs text-zinc-500 mt-1">
              {report.results.tasks_failed}/{report.results.tasks_submitted} failed
            </div>
          </div>
        </div>

        {/* Latency breakdown table */}
        <div className="mb-6">
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
            Latency Breakdown
          </h3>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 overflow-hidden">
            <table className="w-full">
              <thead>
                <tr className="border-b border-zinc-700/60">
                  <th className="py-2.5 pr-4 pl-4 text-left text-xs font-medium text-zinc-500 uppercase tracking-wide">
                    Phase
                  </th>
                  <th className="py-2.5 px-3 text-right text-xs font-medium text-zinc-500 uppercase tracking-wide">
                    p50
                  </th>
                  <th className="py-2.5 px-3 text-right text-xs font-medium text-amber-500/70 uppercase tracking-wide">
                    p95
                  </th>
                  <th className="py-2.5 px-3 text-right text-xs font-medium text-red-500/70 uppercase tracking-wide">
                    p99
                  </th>
                  <th className="py-2.5 px-3 text-right text-xs font-medium text-zinc-500 uppercase tracking-wide">
                    Min
                  </th>
                  <th className="py-2.5 pl-3 pr-4 text-right text-xs font-medium text-zinc-500 uppercase tracking-wide">
                    Max
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/40">
                <LatencyRow label="API Request" bucket={report.results.latency_ms.api_request} />
                <LatencyRow label="Queue Wait" bucket={report.results.latency_ms.submit_to_processing} />
                <LatencyRow label="Processing" bucket={report.results.latency_ms.processing_duration} />
                <LatencyRow label="End-to-End" bucket={report.results.latency_ms.end_to_end} />
              </tbody>
            </table>
          </div>
        </div>

        {/* Queue Behavior */}
        <div className="mb-6">
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
            Queue Behavior
          </h3>
          <div className="grid grid-cols-3 gap-4">
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="text-xs text-zinc-500 mb-1">Peak Backlog</div>
              <div className="text-xl font-bold text-zinc-100 tabular-nums">
                {report.queue_metrics.max_visible_messages}
              </div>
            </div>
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="text-xs text-zinc-500 mb-1">Peak In-Flight</div>
              <div className="text-xl font-bold text-zinc-100 tabular-nums">
                {report.queue_metrics.max_inflight_messages}
              </div>
            </div>
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="text-xs text-zinc-500 mb-1">Converged</div>
              <div className="flex items-center gap-2">
                {report.queue_metrics.backlog_converged ? (
                  <>
                    <CheckCircle2 className="size-4 text-emerald-400" />
                    <span className="text-lg font-semibold text-emerald-400">Yes</span>
                  </>
                ) : (
                  <>
                    <XCircle className="size-4 text-red-400" />
                    <span className="text-lg font-semibold text-red-400">No</span>
                  </>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Recommendations */}
        <div>
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
            Recommendations
          </h3>
          <div className="space-y-3">
            {/* Visibility Timeout */}
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-zinc-300">Visibility Timeout</span>
                <div className="flex items-center gap-2 text-sm tabular-nums">
                  <span className="text-zinc-400">{report.recommendations.visibility_timeout.current}s</span>
                  <span className="text-zinc-600">-&gt;</span>
                  <span className="text-blue-400 font-semibold">{report.recommendations.visibility_timeout.recommended}s</span>
                </div>
              </div>
              <div className="text-xs text-zinc-500 font-mono">
                {report.recommendations.visibility_timeout.formula}
              </div>
            </div>

            {/* Queue Depth */}
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-zinc-300">Queue Max Depth</span>
                <span className="text-sm text-zinc-400 tabular-nums">
                  Current: {report.recommendations.queue_max_depth.current}
                </span>
              </div>
              <div className="text-xs text-zinc-500">
                <span className="text-zinc-400">{report.recommendations.queue_max_depth.recommendation}</span>
                {" -- "}
                {report.recommendations.queue_max_depth.reason}
              </div>
            </div>

            {/* Worker Scaling */}
            <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-zinc-300">Worker Scaling</span>
                <span className="text-sm text-zinc-400 tabular-nums">
                  Concurrency: {report.recommendations.worker_concurrency.current}
                </span>
              </div>
              <div className="text-xs text-zinc-500">
                {report.recommendations.worker_concurrency.observation}
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
});

export { CapacityReportInner as CapacityReport };
