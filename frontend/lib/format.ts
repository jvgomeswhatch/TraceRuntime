export function formatDurationNanos(nanos: number): string {
  const ms = nanos / 1_000_000;
  if (ms < 1) return `${Math.round(nanos / 1000)}µs`;
  return formatDurationMs(ms);
}

export function formatDurationMs(ms: number): string {
  if (ms <= 0) return "---";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return formatDurationSec(ms / 1000);
}

export function formatDurationSec(seconds: number): string {
  if (seconds <= 0) return "---";
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.round(seconds % 60);
    return s > 0 ? `${m}m ${s}s` : `${m}m`;
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${String(m).padStart(2, "0")}m` : `${h}h`;
}

export function formatThroughput(rps: number): { value: string; unit: string } {
  if (rps >= 1) return { value: rps.toFixed(1), unit: "req/s" };
  const rpm = rps * 60;
  if (rpm >= 1) return { value: rpm.toFixed(1), unit: "tasks/min" };
  const rph = rps * 3600;
  return { value: rph.toFixed(0), unit: "tasks/h" };
}

export function formatPercent(value: number): string {
  return `${value.toFixed(1)}%`;
}

export function formatNumber(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

export function timeAgo(dateStr: string): string {
  const diff = (Date.now() - new Date(dateStr).getTime()) / 1000;
  if (diff < 0) return "just now";
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

export function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const day = d.getDate();
  const mon = months[d.getMonth()];
  const year = d.getFullYear();
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${day} ${mon} ${year} • ${hh}:${mm}`;
}
