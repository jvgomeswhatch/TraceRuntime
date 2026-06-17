import { Activity } from "lucide-react";
import { DashboardClient } from "@/components/dashboard-client";

export default function Page() {
  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-6 lg:px-10 lg:py-8">
      <header className="mb-8 flex items-end justify-between">
        <div>
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center size-9 rounded-lg bg-emerald-500/10 border border-emerald-500/20">
              <Activity className="size-5 text-emerald-400" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight bg-gradient-to-r from-zinc-100 via-zinc-100 to-emerald-400 bg-clip-text text-transparent">
              TraceRuntime
            </h1>
          </div>
          <p className="text-sm text-zinc-400 mt-2 ml-12">
            Operations Dashboard
          </p>
        </div>
        <div className="hidden sm:flex items-center gap-2 text-xs text-zinc-400">
          <span className="inline-block size-1.5 rounded-full bg-emerald-500/60" />
          Local Runtime
        </div>
      </header>
      <DashboardClient />
    </main>
  );
}
