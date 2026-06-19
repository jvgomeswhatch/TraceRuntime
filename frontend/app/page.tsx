import { DashboardClient } from "@/components/dashboard-client";
import { OverviewHeader } from "@/components/overview-header";

export default function Page() {
  return (
    <div className="px-6 py-5 lg:px-8">
      <OverviewHeader />
      <DashboardClient />
    </div>
  );
}
