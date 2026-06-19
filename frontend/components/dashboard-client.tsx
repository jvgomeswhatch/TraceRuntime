"use client";

import { StatusCards } from "@/components/status-cards";
import { KpiCards } from "@/components/kpi-cards";
import { SystemTimeline } from "@/components/system-timeline";
import { AutoHealingSummary } from "@/components/auto-healing-summary";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";
import { CapacityReport } from "@/components/capacity-report";

export function DashboardClient() {
  return (
    <div className="space-y-4">
      {/* Section 1: System Status */}
      <StatusCards />

      {/* Section 2: Operational KPIs */}
      <KpiCards />

      {/* Section 3: System Timeline + Auto-Healing */}
      <div className="grid grid-cols-1 xl:grid-cols-12 gap-4">
        <div className="xl:col-span-9">
          <SystemTimeline />
        </div>
        <div className="xl:col-span-3">
          <AutoHealingSummary />
        </div>
      </div>

      {/* Section 4: Event Stream + Capacity Report + Create Task */}
      <div className="grid grid-cols-1 xl:grid-cols-12 gap-4">
        <div className="xl:col-span-5">
          <EventFeed />
        </div>
        <div className="xl:col-span-4">
          <CapacityReport />
        </div>
        <div className="xl:col-span-3">
          <TaskForm />
        </div>
      </div>
    </div>
  );
}
