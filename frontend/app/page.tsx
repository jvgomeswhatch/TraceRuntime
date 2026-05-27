import { DashboardClient } from "@/components/dashboard-client";

export default function Page() {
  return (
    <main className="min-h-screen bg-zinc-950 p-6">
      <header className="mb-6">
        <h1 className="text-xl font-mono font-bold text-zinc-100 tracking-tight">
          TraceRuntime
        </h1>
        <p className="text-xs font-mono text-zinc-500 mt-1">
          Operations Dashboard · Phase 1
        </p>
      </header>
      <DashboardClient />
    </main>
  );
}
