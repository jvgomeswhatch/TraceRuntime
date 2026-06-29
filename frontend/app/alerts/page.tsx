"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Bell, Inbox, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

interface Alert {
  id: string;
  event_type: string;
  severity: string;
  source: string;
  status: string;
  worker_id?: string;
  details: Record<string, unknown>;
  created_at: string;
  resolved_at?: string;
  duration_seconds?: number;
}

interface AlertStatsData {
  active: number;
  acknowledged: number;
  resolved_24h: number;
  critical_active: number;
  avg_resolution_seconds: number;
  by_type: Record<string, { active: number; total_24h: number }>;
}

function severityBadgeClass(severity: string): string {
  if (severity === "critical")
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (severity === "warning")
    return "text-amber-400 border-amber-500/40 bg-amber-500/10";
  return "text-blue-400 border-blue-500/40 bg-blue-500/10";
}

function statusBadgeClass(status: string): string {
  if (status === "active")
    return "text-red-400 border-red-500/40 bg-red-500/10";
  if (status === "acknowledged")
    return "text-amber-400 border-amber-500/40 bg-amber-500/10";
  return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
}

function timeAgo(iso: string): string {
  const diff = (Date.now() - new Date(iso).getTime()) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function formatResolution(seconds: number): string {
  if (seconds <= 0) return "—";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`;
}

export default function AlertsPage() {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [stats, setStats] = useState<AlertStatsData | null>(null);
  const [cursor, setCursor] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [severityFilter, setSeverityFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState("");
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(false);

  const fetchAlerts = useCallback(
    async (append = false) => {
      setLoading(true);
      const params = new URLSearchParams({ status: statusFilter, limit: "50" });
      if (severityFilter) params.set("severity", severityFilter);
      if (typeFilter) params.set("event_type", typeFilter);
      if (append && cursor) params.set("cursor", cursor);

      const res = await fetch(`${API_URL}/api/alerts?${params}`, {
        headers: TOKEN ? { "X-Internal-Token": TOKEN } : {},
      });
      const data = await res.json();
      setAlerts(append ? (prev) => [...prev, ...(data.alerts || [])] : data.alerts || []);
      setCursor(data.next_cursor || "");
      setLoading(false);
    },
    [statusFilter, severityFilter, typeFilter, cursor]
  );

  const fetchStats = useCallback(async () => {
    const res = await fetch(`${API_URL}/api/alerts/stats`, {
      headers: TOKEN ? { "X-Internal-Token": TOKEN } : {},
    });
    const data = await res.json();
    setStats(data);
  }, []);

  useEffect(() => {
    fetchAlerts();
    fetchStats();
  }, [statusFilter, severityFilter, typeFilter]);

  useEffect(() => {
    const interval = setInterval(fetchStats, 10000);
    return () => clearInterval(interval);
  }, [fetchStats]);

  const acknowledge = async (id: string) => {
    await fetch(`${API_URL}/api/alerts/${id}/acknowledge`, {
      method: "POST",
      headers: TOKEN ? { "X-Internal-Token": TOKEN } : {},
    });
    setAlerts((prev) =>
      prev.map((a) => (a.id === id ? { ...a, status: "acknowledged" } : a))
    );
    fetchStats();
  };

  const filtered = useMemo(() => {
    if (!filter.trim()) return alerts;
    const q = filter.toLowerCase();
    return alerts.filter(
      (a) =>
        a.event_type.toLowerCase().includes(q) ||
        a.severity.toLowerCase().includes(q) ||
        (a.worker_id && a.worker_id.toLowerCase().includes(q)) ||
        a.id.toLowerCase().includes(q)
    );
  }, [alerts, filter]);

  return (
    <div className="px-6 py-5 lg:px-8">
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
            <Bell className="size-5 text-zinc-400" />
            Alert Center
          </h1>
          <p className="text-sm text-zinc-500 mt-0.5">
            Operational alerts and healing events
          </p>
        </div>
        {alerts.length > 0 && (
          <div className="flex items-center gap-2 text-xs font-medium text-amber-400 border border-amber-500/30 bg-amber-500/5 rounded-lg px-3 py-1.5">
            <Bell className="size-3.5" />
            <span className="tabular-nums">{alerts.length}</span> alerts
          </div>
        )}
      </header>

      {/* Stats */}
      {stats && (
        <div className="grid grid-cols-4 gap-3 mb-5">
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Active</p>
            <p className="text-lg font-semibold text-red-400 tabular-nums">{stats.active}</p>
          </div>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Acknowledged</p>
            <p className="text-lg font-semibold text-amber-400 tabular-nums">{stats.acknowledged}</p>
          </div>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Resolved (24h)</p>
            <p className="text-lg font-semibold text-emerald-400 tabular-nums">{stats.resolved_24h}</p>
          </div>
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3">
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">Avg Resolution</p>
            <p className="text-lg font-semibold text-zinc-100 tabular-nums">
              {formatResolution(stats.avg_resolution_seconds)}
            </p>
          </div>
        </div>
      )}

      {/* Filters */}
      <div className="mb-4 flex items-center gap-3">
        <div className="relative max-w-xs flex-1">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-zinc-500" />
          <Input
            type="text"
            placeholder="Filter by type, severity, worker..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="pl-8 bg-zinc-900/60 border-zinc-800/80 text-zinc-100 placeholder:text-zinc-500 text-xs h-8"
          />
        </div>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="h-8 rounded-lg border border-zinc-800/80 bg-zinc-900/60 px-2.5 text-xs text-zinc-300 focus:outline-none focus:ring-1 focus:ring-zinc-700"
        >
          <option value="all">All Status</option>
          <option value="active">Active</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="resolved">Resolved</option>
        </select>
        <select
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value)}
          className="h-8 rounded-lg border border-zinc-800/80 bg-zinc-900/60 px-2.5 text-xs text-zinc-300 focus:outline-none focus:ring-1 focus:ring-zinc-700"
        >
          <option value="">All Severity</option>
          <option value="critical">Critical</option>
          <option value="warning">Warning</option>
          <option value="info">Info</option>
        </select>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="h-8 rounded-lg border border-zinc-800/80 bg-zinc-900/60 px-2.5 text-xs text-zinc-300 focus:outline-none focus:ring-1 focus:ring-zinc-700"
        >
          <option value="">All Types</option>
          <option value="worker.stale">worker.stale</option>
          <option value="worker.down">worker.down</option>
          <option value="queue.lag">queue.lag</option>
          <option value="task.stuck">task.stuck</option>
          <option value="dlq.nonempty">dlq.nonempty</option>
        </select>
      </div>

      {/* Table */}
      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Inbox className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">
            {alerts.length === 0
              ? "No alerts recorded yet."
              : "No alerts match your filters."}
          </p>
        </div>
      ) : (
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-zinc-800/60">
                <th className="py-2.5 pl-4 pr-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Severity
                </th>
                <th className="py-2.5 px-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Type
                </th>
                <th className="py-2.5 px-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Source
                </th>
                <th className="py-2.5 px-3 text-left text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Status
                </th>
                <th className="py-2.5 px-3 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Time
                </th>
                <th className="py-2.5 pl-3 pr-4 text-right text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
                  Action
                </th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((alert) => (
                <tr
                  key={alert.id}
                  className="border-b border-zinc-800/40 last:border-b-0 hover:bg-zinc-800/30 transition-colors"
                >
                  <td className="py-2.5 pl-4 pr-3">
                    <Badge
                      variant="outline"
                      className={`${severityBadgeClass(alert.severity)} text-[10px] uppercase font-semibold px-1.5 py-0`}
                    >
                      {alert.severity}
                    </Badge>
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-300 font-mono">
                    {alert.event_type}
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-500">
                    {alert.worker_id ? (
                      <span title={alert.worker_id}>Worker</span>
                    ) : (
                      alert.source
                    )}
                  </td>
                  <td className="py-2.5 px-3">
                    <Badge
                      variant="outline"
                      className={`${statusBadgeClass(alert.status)} text-[10px] uppercase font-semibold px-1.5 py-0`}
                    >
                      {alert.status}
                    </Badge>
                  </td>
                  <td className="py-2.5 px-3 text-xs text-zinc-500 tabular-nums text-right">
                    {timeAgo(alert.created_at)}
                  </td>
                  <td className="py-2.5 pl-3 pr-4 text-right">
                    {alert.status === "active" && (
                      <button
                        onClick={() => acknowledge(alert.id)}
                        className="text-[10px] font-medium text-amber-400 hover:text-amber-300 border border-amber-500/30 bg-amber-500/5 hover:bg-amber-500/10 rounded px-2 py-0.5 transition-colors"
                      >
                        Ack
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Load More */}
      {cursor && (
        <div className="mt-4 flex justify-center">
          <button
            onClick={() => fetchAlerts(true)}
            disabled={loading}
            className="text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 bg-zinc-900/60 rounded-lg px-4 py-2 transition-colors disabled:opacity-50"
          >
            {loading ? "Loading..." : "Load More"}
          </button>
        </div>
      )}
    </div>
  );
}
