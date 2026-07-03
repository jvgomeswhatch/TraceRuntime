"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { formatDurationSec } from "@/lib/format";
import {
  Zap,
  RefreshCw,
  Play,
  CheckCircle,
  AlertTriangle,
  XCircle,
  Trash2,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

const SCENARIOS = [
  "worker-crash",
  "runtime-hang",
  "ai-failure",
  "postgres-failure",
  "queue-flood",
  "slow-inference",
  "all",
] as const;

interface ReportSummary {
  total: number;
  passed: number;
  warned: number;
  failed: number;
}

interface ChaosReport {
  id: string;
  file: string;
  timestamp: string;
  duration_seconds: number;
  summary: ReportSummary;
  scenarios: string[];
}

interface TriggerState {
  run_id: string;
  status: "idle" | "queued" | "completed" | "failed";
  command?: string;
  summary?: ReportSummary;
  report_id?: string;
}

const formatDuration = formatDurationSec;

function summaryBadgeClass(summary: ReportSummary): string {
  if (summary.failed > 0)
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (summary.warned > 0)
    return "text-amber-400 border-amber-500/40 bg-amber-500/10";
  return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
}

function summaryLabel(summary: ReportSummary): string {
  if (summary.failed > 0) return "FAIL";
  if (summary.warned > 0) return "WARN";
  return "PASS";
}

const apiHeaders: Record<string, string> = TOKEN
  ? { "X-Internal-Token": TOKEN }
  : {};

async function fetchReportsApi(): Promise<ChaosReport[]> {
  const res = await fetch(`${API_URL}/api/chaos/reports`, { headers: apiHeaders });
  if (!res.ok) return [];
  const data = await res.json();
  return data.reports || [];
}

async function fetchChaosStatus(runId?: string) {
  const url = runId
    ? `${API_URL}/api/chaos/status?run_id=${encodeURIComponent(runId)}`
    : `${API_URL}/api/chaos/status`;
  const res = await fetch(url, { headers: apiHeaders });
  if (!res.ok) return null;
  return res.json();
}

export default function ChaosPage() {
  const [reports, setReports] = useState<ChaosReport[]>([]);
  const [loading, setLoading] = useState(false);
  const [scenario, setScenario] = useState<string>("worker-crash");
  const [trigger, setTrigger] = useState<TriggerState>({ run_id: "", status: "idle" });
  const [reportsVersion, setReportsVersion] = useState(0);

  // Fetch reports on mount, on filter change, and when reportsVersion bumps
  useEffect(() => {
    let cancelled = false;

    async function doFetch() {
      setLoading(true);
      try {
        const data = await fetchReportsApi();
        if (!cancelled) setReports(data);
      } catch {
        /* ignore */
      }
      if (!cancelled) setLoading(false);
    }

    doFetch();

    const interval = setInterval(async () => {
      try {
        const data = await fetchReportsApi();
        if (!cancelled) setReports(data);
      } catch {
        /* ignore */
      }
    }, 10000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [reportsVersion]);

  // Restore queued trigger state on mount
  useEffect(() => {
    async function restoreState() {
      try {
        const data = await fetchChaosStatus();
        if (data?.status === "queued" && data.run_id) {
          setTrigger({
            run_id: data.run_id,
            status: "queued",
            command: data.command,
          });
        }
      } catch {
        /* ignore */
      }
    }
    restoreState();
  }, []);

  // Poll for trigger completion
  useEffect(() => {
    if (trigger.status !== "queued" || !trigger.run_id) return;

    const runId = trigger.run_id;
    const interval = setInterval(async () => {
      try {
        const data = await fetchChaosStatus(runId);
        if (data && (data.status === "completed" || data.status === "failed")) {
          const reportId = data.report_url
            ? data.report_url.replace("/api/chaos/reports/", "")
            : "";
          setTrigger({
            run_id: runId,
            status: data.status,
            summary: data.summary,
            report_id: reportId,
          });
          setReportsVersion((v) => v + 1);
        }
      } catch {
        /* ignore */
      }
    }, 3000);
    return () => clearInterval(interval);
  }, [trigger.status, trigger.run_id]);

  // Auto-dismiss completed/failed trigger after 10s
  useEffect(() => {
    if (trigger.status !== "completed" && trigger.status !== "failed") return;
    const timer = setTimeout(() => {
      setTrigger({ run_id: "", status: "idle" });
    }, 10000);
    return () => clearTimeout(timer);
  }, [trigger.status]);

  const refreshReports = async () => {
    setLoading(true);
    try {
      const data = await fetchReportsApi();
      setReports(data);
    } catch {
      /* ignore */
    }
    setLoading(false);
  };

  const cancelTrigger = async () => {
    if (trigger.run_id) {
      try {
        await fetch(`${API_URL}/api/chaos/requests/${encodeURIComponent(trigger.run_id)}`, {
          method: "DELETE",
          headers: apiHeaders,
        });
      } catch {
        /* ignore */
      }
    }
    setTrigger({ run_id: "", status: "idle" });
  };

  const deleteReport = async (reportId: string) => {
    try {
      const res = await fetch(
        `${API_URL}/api/chaos/reports/${encodeURIComponent(reportId)}`,
        { method: "DELETE", headers: apiHeaders }
      );
      if (res.ok) {
        setReports((prev) => prev.filter((r) => r.id !== reportId));
      }
    } catch {
      /* ignore */
    }
  };

  const triggerScenario = async () => {
    try {
      const res = await fetch(`${API_URL}/api/chaos/trigger`, {
        method: "POST",
        headers: { ...apiHeaders, "Content-Type": "application/json" },
        body: JSON.stringify({ scenario }),
      });
      if (res.ok) {
        const data = await res.json();
        setTrigger({ run_id: data.run_id, status: "queued", command: data.command });
      }
    } catch {
      /* ignore */
    }
  };

  return (
    <div className="px-6 py-5 lg:px-8">
      {/* Header -- same pattern as DLQ/Alerts */}
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <Zap className="size-5 text-zinc-400" />
            Chaos Dashboard
          </h1>
          <p className="text-sm text-zinc-500 mt-0.5">
            Trigger chaos scenarios and review resilience reports
          </p>
        </div>
        <button
          onClick={refreshReports}
          disabled={loading}
          className="inline-flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 bg-zinc-900/60 rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50"
        >
          <RefreshCw className={`size-3 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </header>

      {/* Trigger form -- card style matching stats cards */}
      <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-5 mb-5">
        <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide mb-3">
          Trigger Scenario
        </p>
        <div className="flex items-end gap-4 flex-wrap">
          <div className="flex-1 min-w-[200px]">
            <label className="text-xs text-zinc-400 block mb-1.5">
              Scenario
            </label>
            <select
              value={scenario}
              onChange={(e) => setScenario(e.target.value)}
              className="w-full h-8 rounded-lg border border-zinc-800/80 bg-zinc-900/60 px-3 text-xs text-zinc-300 focus:outline-none focus:ring-1 focus:ring-zinc-700"
            >
              {SCENARIOS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </div>
          <button
            onClick={triggerScenario}
            disabled={trigger.status === "queued"}
            className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-400 hover:text-emerald-300 border border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10 rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50"
          >
            <Play className="size-3" />
            Run Chaos
          </button>
        </div>

        {trigger.status === "queued" && (
          <div className="mt-4 rounded-lg bg-amber-500/5 border border-amber-500/20 p-4 space-y-2">
            <div className="text-xs text-amber-400 flex items-center gap-2 font-medium">
              <span className="inline-block size-2 rounded-full bg-amber-400 animate-pulse" />
              Queued --- waiting for execution
            </div>
            <p className="text-xs text-zinc-400">
              Run this command in the project root terminal:
            </p>
            <code className="block text-xs text-zinc-200 bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 font-mono select-all">
              {trigger.command || "make chaos"}
            </code>
            <div className="flex items-center justify-between">
              <p className="text-xs text-zinc-500">
                The result will appear here automatically when done.
              </p>
              <button
                onClick={cancelTrigger}
                className="inline-flex items-center gap-1.5 text-xs font-medium text-red-400 hover:text-red-300 border border-red-500/30 bg-red-500/5 hover:bg-red-500/10 rounded-lg px-3 py-1.5 transition-colors"
              >
                <XCircle className="size-3" />
                Cancel
              </button>
            </div>
          </div>
        )}
        {(trigger.status === "completed" || trigger.status === "failed") && trigger.summary && (
          <div className={`mt-4 rounded-lg p-4 ${
            trigger.status === "failed"
              ? "bg-red-500/5 border border-red-500/20"
              : "bg-emerald-500/5 border border-emerald-500/20"
          }`}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-4">
                <span className={`text-xs flex items-center gap-1.5 font-medium ${
                  trigger.status === "failed" ? "text-red-400" : "text-emerald-400"
                }`}>
                  {trigger.status === "failed"
                    ? <XCircle className="size-3.5" />
                    : <CheckCircle className="size-3.5" />}
                  {trigger.status === "failed" ? "Failed" : "Completed"}
                </span>
                <div className="flex items-center gap-3">
                  {trigger.summary.passed > 0 && (
                    <span className="flex items-center gap-1 text-xs text-emerald-400">
                      <CheckCircle className="size-3" />
                      {trigger.summary.passed} passed
                    </span>
                  )}
                  {trigger.summary.warned > 0 && (
                    <span className="flex items-center gap-1 text-xs text-amber-400">
                      <AlertTriangle className="size-3" />
                      {trigger.summary.warned} warned
                    </span>
                  )}
                  {trigger.summary.failed > 0 && (
                    <span className="flex items-center gap-1 text-xs text-red-400">
                      <XCircle className="size-3" />
                      {trigger.summary.failed} failed
                    </span>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-3">
                <Link
                  href={`/chaos/${trigger.report_id || trigger.run_id}`}
                  className={`text-xs underline ${
                    trigger.status === "failed"
                      ? "text-red-400 hover:text-red-300"
                      : "text-emerald-400 hover:text-emerald-300"
                  }`}
                >
                  View Report
                </Link>
                <button
                  onClick={() => setTrigger({ run_id: "", status: "idle" })}
                  className="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-400 hover:text-zinc-200 border border-zinc-700 bg-zinc-800/50 hover:bg-zinc-800 rounded-lg px-3 py-1.5 transition-colors"
                >
                  Dismiss
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Stats row -- matching Alerts/DLQ pattern */}
      {reports.length > 0 && (
        <div className="grid grid-cols-3 gap-3 mb-5">
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
              Total Reports
            </p>
            <p className="text-lg font-semibold text-zinc-100 tabular-nums">
              {reports.length}
            </p>
          </div>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
              All Passed
            </p>
            <p className="text-lg font-semibold text-emerald-400 tabular-nums">
              {reports.filter((r) => r.summary.failed === 0 && r.summary.warned === 0).length}
            </p>
          </div>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
              With Failures
            </p>
            <p className="text-lg font-semibold text-red-400 tabular-nums">
              {reports.filter((r) => r.summary.failed > 0).length}
            </p>
          </div>
        </div>
      )}

      {/* Reports list -- card style matching DLQ messages */}
      {reports.length === 0 && !loading ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Zap className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">No chaos reports found</p>
          <p className="text-xs text-zinc-600 mt-1">
            Run &apos;make chaos&apos; to generate reports
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {reports.map((report) => (
            <div
              key={report.id}
              className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 hover:border-zinc-700 transition-colors"
            >
              <div className="flex items-start justify-between mb-2">
                <Link href={`/chaos/${report.id}`} className="space-y-1.5 min-w-0 flex-1">
                  <div className="flex items-center gap-2.5">
                    <span className="text-xs font-mono text-zinc-200">
                      {report.id}
                    </span>
                    <Badge
                      variant="outline"
                      className={`text-[10px] uppercase font-semibold px-1.5 py-0 ${summaryBadgeClass(report.summary)}`}
                    >
                      {summaryLabel(report.summary)}
                    </Badge>
                  </div>
                  <p className="text-xs text-zinc-500">
                    {report.timestamp
                      ? new Date(report.timestamp).toLocaleString()
                      : "---"}
                    {report.duration_seconds > 0 &&
                      ` · ${formatDuration(report.duration_seconds)}`}
                    {` · ${report.summary.total} scenarios`}
                  </p>
                </Link>
                <button
                  onClick={() => deleteReport(report.id)}
                  className="ml-3 p-2 rounded-lg border border-zinc-700 bg-zinc-100 text-zinc-700 hover:text-red-500 hover:border-red-400 hover:bg-red-50 transition-colors"
                  title="Delete report"
                >
                  <Trash2 className="size-3.5" />
                </button>
              </div>

              <div className="flex items-center gap-3">
                {report.summary.passed > 0 && (
                  <span className="flex items-center gap-1 text-xs text-emerald-400">
                    <CheckCircle className="size-3" />
                    {report.summary.passed} passed
                  </span>
                )}
                {report.summary.warned > 0 && (
                  <span className="flex items-center gap-1 text-xs text-amber-400">
                    <AlertTriangle className="size-3" />
                    {report.summary.warned} warned
                  </span>
                )}
                {report.summary.failed > 0 && (
                  <span className="flex items-center gap-1 text-xs text-red-400">
                    <XCircle className="size-3" />
                    {report.summary.failed} failed
                  </span>
                )}
                {report.scenarios.length > 0 && (
                  <span className="text-xs text-zinc-600 ml-auto">
                    {report.scenarios.join(", ")}
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
