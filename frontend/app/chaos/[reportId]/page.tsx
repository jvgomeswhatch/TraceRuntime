"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  ArrowLeft,
  CheckCircle,
  AlertTriangle,
  XCircle,
  Zap,
  Loader2,
  Inbox,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

interface SLOResult {
  name: string;
  type: string;
  expected: number | string;
  actual: number | string;
  status: "PASS" | "WARN" | "FAIL";
}

interface StageResult {
  duration_ms: number;
  status: string;
}

interface ScenarioReport {
  name: string;
  status: "PASS" | "WARN" | "FAIL";
  duration_seconds: number;
  stages: Record<string, StageResult>;
  metrics: Record<string, unknown>;
  slo_results: SLOResult[];
  warnings: string[];
  error?: string;
}

interface SuiteReport {
  version: string;
  run_id: string;
  git_commit: string;
  environment: string;
  timestamp: string;
  duration_seconds: number;
  summary: { total: number; passed: number; warned: number; failed: number };
  scenarios: ScenarioReport[];
}

const STAGE_ORDER = ["setup", "inject", "observe", "validate", "cleanup"];

function statusBadgeClass(status: string): string {
  if (status === "FAIL")
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (status === "WARN")
    return "text-amber-400 border-amber-500/40 bg-amber-500/10";
  return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
}

function statusAccentBar(status: string): string {
  if (status === "FAIL") return "bg-red-500";
  if (status === "WARN") return "bg-amber-500";
  return "bg-emerald-500";
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case "PASS":
      return <CheckCircle className="size-4 text-emerald-400" />;
    case "WARN":
      return <AlertTriangle className="size-4 text-amber-400" />;
    case "FAIL":
      return <XCircle className="size-4 text-red-400" />;
    default:
      return <span className="size-4" />;
  }
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function formatSloValue(val: number | string): string {
  const s = String(val);
  if (s === "true" || s === "false") return s;
  const num = parseFloat(s);
  if (!isNaN(num)) {
    return Number.isInteger(num) ? `${num}s` : `${num.toFixed(1)}s`;
  }
  return s;
}

function isTimeMetric(key: string): boolean {
  return /seconds?|time|duration|latency|_s$/i.test(key);
}

function formatMetricValue(val: unknown, key = ""): string {
  if (val === null || val === undefined) return "—";
  if (typeof val === "object") {
    return Object.entries(val as Record<string, unknown>)
      .map(([k, v]) => `${k}: ${formatMetricValue(v, k)}`)
      .join(", ");
  }
  const s = String(val);
  if (s === "true" || s === "false") return s;
  const num = parseFloat(s);
  if (!isNaN(num) && s.match(/^[\d.]+$/)) {
    if (isTimeMetric(key)) {
      return Number.isInteger(num) ? `${num}s` : `${num.toFixed(1)}s`;
    }
    return Number.isInteger(num) ? `${num}` : `${num.toFixed(1)}`;
  }
  return s;
}

function isBooleanSlo(val: number | string): boolean {
  const s = String(val);
  return s === "true" || s === "false";
}

export default function ChaosReportDetailPage() {
  const params = useParams();
  const reportId = params.reportId as string;
  const [report, setReport] = useState<SuiteReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    const fetchReport = async () => {
      try {
        const res = await fetch(`${API_URL}/api/chaos/reports/${reportId}`, {
          headers: TOKEN ? { "X-Internal-Token": TOKEN } : {},
        });
        if (!res.ok) {
          setError(
            res.status === 404 ? "Report not found" : "Failed to load report"
          );
          setLoading(false);
          return;
        }
        setReport(await res.json());
      } catch {
        setError("Failed to load report");
      }
      setLoading(false);
    };
    fetchReport();
  }, [reportId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <Loader2 className="size-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="px-6 py-5 lg:px-8">
        <Link
          href="/chaos"
          className="flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300 transition-colors mb-5"
        >
          <ArrowLeft className="size-3" />
          Back to Chaos Dashboard
        </Link>
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <AlertTriangle className="size-8 text-red-500/60 mb-3" />
          <p className="text-sm text-red-400">{error}</p>
          <p className="text-xs text-zinc-600 mt-1">Report ID: {reportId}</p>
        </div>
      </div>
    );
  }

  if (!report) return null;

  const overallStatus =
    report.summary.failed > 0
      ? "FAIL"
      : report.summary.warned > 0
        ? "WARN"
        : "PASS";

  return (
    <div className="px-6 py-5 lg:px-8">
      {/* Header — matches trace detail pattern */}
      <header className="mb-4 flex items-center justify-between">
        <div>
          <Link
            href="/chaos"
            className="flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300 transition-colors mb-1.5"
          >
            <ArrowLeft className="size-3" />
            Back to Chaos Dashboard
          </Link>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <Zap className="size-5 text-zinc-400" />
            Chaos Report
          </h1>
          <p className="text-xs text-zinc-500 mt-0.5">
            {new Date(report.timestamp).toLocaleString()}
            {` · ${formatDuration(report.duration_seconds)}`}
            {report.git_commit && ` · Git: ${report.git_commit.slice(0, 7)}`}
            {report.environment && ` · ${report.environment}`}
          </p>
        </div>
        <Badge
          variant="outline"
          className={`text-[10px] uppercase font-semibold px-1.5 py-0 ${statusBadgeClass(overallStatus)}`}
        >
          {overallStatus}
        </Badge>
      </header>

      {/* Summary stats — 4-column grid matching trace detail */}
      <div className="grid grid-cols-4 gap-3 mb-5">
        {[
          {
            label: "Total",
            value: report.summary.total,
            color: "text-zinc-100",
            accent: statusAccentBar(overallStatus),
          },
          {
            label: "Passed",
            value: report.summary.passed,
            color: "text-emerald-400",
            accent: "bg-emerald-500",
          },
          {
            label: "Warned",
            value: report.summary.warned,
            color: "text-amber-400",
            accent: "bg-amber-500",
          },
          {
            label: "Failed",
            value: report.summary.failed,
            color: "text-red-400",
            accent: "bg-red-500",
          },
        ].map((item) => (
          <div
            key={item.label}
            className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3 overflow-hidden"
          >
            <div
              className={`absolute top-0 left-0 right-0 h-[2px] ${item.accent}`}
            />
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
              {item.label}
            </p>
            <p className={`text-lg font-semibold tabular-nums ${item.color}`}>
              {item.value}
            </p>
          </div>
        ))}
      </div>

      {/* Report ID card */}
      <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3 mb-5 overflow-hidden">
        <div
          className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(overallStatus)}`}
        />
        <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide mb-1">
          Run ID
        </p>
        <p className="text-xs font-mono text-zinc-300 break-all">
          {report.run_id}
        </p>
      </div>

      {/* Scenarios */}
      {report.scenarios.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Inbox className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">No scenarios in this report</p>
        </div>
      ) : (
        <div className="space-y-3">
          {report.scenarios.map((sc) => (
            <div
              key={sc.name}
              className="relative rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 overflow-hidden"
            >
              <div
                className={`absolute top-0 left-0 right-0 h-[2px] ${statusAccentBar(sc.status)}`}
              />

              {/* Scenario header */}
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2.5">
                  <StatusIcon status={sc.status} />
                  <span className="text-xs font-mono text-zinc-200">
                    {sc.name}
                  </span>
                  <span className="text-[10px] text-zinc-500 tabular-nums">
                    {formatDuration(sc.duration_seconds)}
                  </span>
                </div>
                <Badge
                  variant="outline"
                  className={`text-[10px] uppercase font-semibold px-1.5 py-0 ${statusBadgeClass(sc.status)}`}
                >
                  {sc.status}
                </Badge>
              </div>

              {/* Metrics */}
              {Object.keys(sc.metrics || {}).length > 0 && (
                <div className="flex gap-4 mb-3 flex-wrap">
                  {Object.entries(sc.metrics).map(([key, val]) => (
                    <div key={key} className="text-xs">
                      <span className="text-zinc-500">
                        {key.replace(/_/g, " ")}:{" "}
                      </span>
                      <span className="text-zinc-300 tabular-nums font-semibold">
                        {formatMetricValue(val as unknown, key)}
                      </span>
                    </div>
                  ))}
                </div>
              )}

              {/* Stages */}
              {sc.stages && Object.keys(sc.stages).length > 0 && (
                <div className="rounded-lg bg-zinc-950/50 p-3 space-y-1 mb-3">
                  {STAGE_ORDER.map((stage) => {
                    const s = sc.stages?.[stage];
                    if (!s) return null;
                    return (
                      <div
                        key={stage}
                        className="flex items-center gap-3 text-xs"
                      >
                        <span
                          className={
                            s.status === "ok"
                              ? "text-emerald-400"
                              : "text-amber-400"
                          }
                        >
                          {s.status === "ok" ? (
                            <CheckCircle className="size-3" />
                          ) : (
                            <AlertTriangle className="size-3" />
                          )}
                        </span>
                        <span className="text-zinc-400 w-16 font-mono">
                          {stage}
                        </span>
                        <span className="text-zinc-500 tabular-nums font-mono">
                          {formatMs(s.duration_ms)}
                        </span>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* SLOs */}
              {sc.slo_results && sc.slo_results.length > 0 && (
                <div className="pt-3 border-t border-zinc-800/40">
                  <p className="text-[10px] text-zinc-500 uppercase tracking-wide font-medium mb-2">
                    SLOs
                  </p>
                  <div className="space-y-2">
                    {sc.slo_results.map((slo, i) => {
                      const sloColor =
                        slo.status === "FAIL"
                          ? { border: "border-red-500/30", bg: "bg-red-500/5", accent: "bg-red-500", actualText: "text-red-400" }
                          : slo.status === "WARN"
                            ? { border: "border-amber-500/30", bg: "bg-amber-500/5", accent: "bg-amber-500", actualText: "text-amber-400" }
                            : { border: "border-emerald-500/30", bg: "bg-emerald-500/5", accent: "bg-emerald-500", actualText: "text-emerald-400" };
                      return (
                        <div
                          key={i}
                          className={`relative rounded-lg border ${sloColor.border} ${sloColor.bg} px-4 py-3 overflow-hidden`}
                        >
                          <div className={`absolute top-0 left-0 right-0 h-[2px] ${sloColor.accent}`} />
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2.5">
                              <StatusIcon status={slo.status} />
                              <span className="text-xs font-medium text-zinc-200">
                                {slo.name.replace(/_/g, " ")}
                              </span>
                            </div>
                            <div className="flex items-center gap-6">
                              <div className="text-right">
                                <p className="text-[10px] uppercase tracking-wide text-zinc-500">
                                  Actual
                                </p>
                                <p className={`text-sm font-bold tabular-nums font-mono ${sloColor.actualText}`}>
                                  {formatSloValue(slo.actual)}
                                </p>
                              </div>
                              {!isBooleanSlo(slo.expected) && (
                                <div className="text-right">
                                  <p className="text-[10px] uppercase tracking-wide text-zinc-500">
                                    Expected
                                  </p>
                                  <p className="text-sm font-bold tabular-nums font-mono text-zinc-400">
                                    {formatSloValue(slo.expected)}
                                  </p>
                                </div>
                              )}
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}

              {/* Warnings */}
              {sc.warnings && sc.warnings.length > 0 && (
                <div className="mt-3 pt-3 border-t border-zinc-800/40">
                  {sc.warnings.map((w, i) => (
                    <p
                      key={i}
                      className="text-xs text-amber-400 flex items-center gap-1.5 mb-1"
                    >
                      <AlertTriangle className="size-3 shrink-0" />
                      {w}
                    </p>
                  ))}
                </div>
              )}

              {/* Error */}
              {sc.error && (
                <div className="mt-3 rounded-lg bg-red-500/5 border border-red-500/20 p-3">
                  <span className="text-[10px] text-red-400 uppercase tracking-wide font-semibold">
                    Error
                  </span>
                  <p className="text-xs text-red-300 font-mono mt-1.5 leading-relaxed">
                    {sc.error}
                  </p>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
