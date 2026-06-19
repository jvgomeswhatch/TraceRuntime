"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { Sidebar } from "@/components/sidebar";

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <SSEProvider>
      <div className="flex min-h-screen">
        <Sidebar />
        <main className="flex-1 ml-[180px] min-h-screen">
          {children}
        </main>
      </div>
    </SSEProvider>
  );
}
