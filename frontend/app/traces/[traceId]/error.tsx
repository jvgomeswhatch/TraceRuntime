"use client";

import Link from "next/link";
import { ArrowLeft, AlertTriangle } from "lucide-react";

export default function TraceError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="px-6 py-5 lg:px-8">
      <Link
        href="/traces"
        className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 transition-colors mb-5"
      >
        <ArrowLeft className="size-3.5" />
        Back to traces
      </Link>
      <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
        <AlertTriangle className="size-8 text-red-500/60 mb-3" />
        <p className="text-sm text-red-400">Failed to load trace</p>
        <p className="text-xs text-zinc-600 mt-1">{error.message}</p>
        <button
          onClick={reset}
          className="mt-4 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-700 rounded-lg px-3 py-1.5 transition-colors"
        >
          Try again
        </button>
      </div>
    </div>
  );
}
