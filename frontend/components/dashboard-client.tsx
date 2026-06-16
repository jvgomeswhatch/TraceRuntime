"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";
import { OperationsSummary } from "@/components/operations-summary";

export function DashboardClient() {
  return (
    <SSEProvider>
      <div className="max-w-6xl space-y-6">
        <OperationsSummary />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <TaskForm />
          <EventFeed />
        </div>
      </div>
    </SSEProvider>
  );
}
