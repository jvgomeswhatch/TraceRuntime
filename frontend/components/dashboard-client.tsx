"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";
import { OperationsSummary } from "@/components/operations-summary";
import { CapacityReport } from "@/components/capacity-report";

export function DashboardClient() {
  return (
    <SSEProvider>
      <div className="w-full space-y-8">
        <OperationsSummary />
        <CapacityReport />
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          <TaskForm />
          <div className="lg:col-span-2">
            <EventFeed />
          </div>
        </div>
      </div>
    </SSEProvider>
  );
}
