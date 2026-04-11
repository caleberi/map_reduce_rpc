import { Link, Outlet, useRouterState } from "@tanstack/react-router"
import { Activity, GitCompare, Hexagon } from "lucide-react"
import { useQuery } from "@tanstack/react-query"
import { cn } from "@/lib/utils"
import { ThemeToggle } from "@/components/theme-toggle"
import { fetchClusterStatus } from "@/lib/api"

const navItems = [
  { to: "/" as const, label: "Simulate", icon: Activity },
  { to: "/compare" as const, label: "Compare", icon: GitCompare },
]

export function DashboardLayout() {
  const router = useRouterState()
  const currentPath = router.location.pathname

  const { data: cluster } = useQuery({
    queryKey: ["cluster-status"],
    queryFn: fetchClusterStatus,
    refetchInterval: 5000,
  })

  const allOnline =
    cluster &&
    cluster.coordinator.online &&
    cluster.master.online &&
    cluster.workers.every((w) => w.online)

  const statusColor = cluster
    ? allOnline
      ? "#10b981"
      : "#f59e0b"
    : "#6b7280"

  const statusText = cluster
    ? allOnline
      ? "System Ready"
      : "Partial"
    : "Probing…"

  return (
    <div className="min-h-screen flex flex-col bg-background">
      {/* Header */}
      <header className="border-b border-border/40 bg-card/40 backdrop-blur-sm relative z-10">
        <div className="flex h-12 items-center gap-6 px-5">
          <div className="flex items-center gap-2 font-semibold text-sm tracking-wider">
            <Hexagon className="h-4 w-4 text-primary" />
            <span className="uppercase text-primary">MapReduce</span>
          </div>
          <nav className="flex items-center gap-1">
            {navItems.map(({ to, label, icon: Icon }) => (
              <Link
                key={to}
                to={to}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                  currentPath === to
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                )}
              >
                <Icon className="h-3.5 w-3.5" />
                {label}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <ThemeToggle />
            <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-75" style={{ backgroundColor: statusColor }} />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: statusColor }} />
              </span>
              {statusText}
            </div>
          </div>
        </div>
      </header>
      {/* Content */}
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>
    </div>
  )
}
