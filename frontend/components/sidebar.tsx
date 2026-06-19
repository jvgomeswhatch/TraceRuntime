"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, LayoutDashboard, ListTodo, GitBranch } from "lucide-react";
import { useSSEContext } from "@/components/providers/sse-provider";

const NAV_ITEMS = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/tasks", label: "Tasks", icon: ListTodo },
  { href: "/traces", label: "Traces", icon: GitBranch },
] as const;

function SystemIndicator() {
  const { connected, workers } = useSSEContext();
  const healthyCount = workers.filter((w) => w.status === "healthy").length;
  const allHealthy = connected && workers.length > 0 && healthyCount === workers.length;

  return (
    <div className="px-3 py-3 border-t border-zinc-800/80">
      <div className="flex items-center gap-2">
        <span
          className={`inline-block size-2 rounded-full ${
            allHealthy
              ? "bg-emerald-400 shadow-sm shadow-emerald-400/50"
              : connected
                ? "bg-amber-400 shadow-sm shadow-amber-400/50"
                : "bg-zinc-500"
          }`}
        />
        <div className="min-w-0">
          <p className="text-xs text-zinc-400 truncate">
            System
          </p>
          <p
            className={`text-xs font-medium ${
              allHealthy
                ? "text-emerald-400"
                : connected
                  ? "text-amber-400"
                  : "text-zinc-500"
            }`}
          >
            {allHealthy ? "Healthy" : connected ? "Degraded" : "Disconnected"}
          </p>
        </div>
      </div>
      <p className="text-[10px] text-zinc-600 mt-1.5 pl-4">v1.0.0</p>
    </div>
  );
}

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="fixed top-0 left-0 h-screen w-[180px] bg-zinc-950 border-r border-zinc-800/80 flex flex-col z-40">
      <div className="px-4 py-5 border-b border-zinc-800/80">
        <Link href="/" className="flex items-center gap-2.5">
          <div className="flex items-center justify-center size-8 rounded-lg bg-emerald-500/10 border border-emerald-500/20">
            <Activity className="size-4 text-emerald-400" />
          </div>
          <div>
            <h1 className="text-sm font-bold text-zinc-100 tracking-tight">
              TraceRuntime
            </h1>
            <p className="text-[10px] text-zinc-500 leading-none mt-0.5">
              AI Runtime Platform
            </p>
          </div>
        </Link>
      </div>

      <nav className="flex-1 px-2 py-3 space-y-0.5">
        {NAV_ITEMS.map((item) => {
          const isActive = pathname === item.href;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? "bg-emerald-500/10 text-emerald-400"
                  : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/50"
              }`}
            >
              <item.icon className="size-4" />
              {item.label}
            </Link>
          );
        })}
      </nav>

      <SystemIndicator />
    </aside>
  );
}
