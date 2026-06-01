"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";

export function DashboardClient() {
  return (
    <SSEProvider>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-6xl">
        <TaskForm />
        <EventFeed />
      </div>
    </SSEProvider>
  );
}
