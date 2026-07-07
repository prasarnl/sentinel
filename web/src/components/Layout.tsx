import { NavLink, Outlet } from "react-router-dom";
import { LayoutGrid, Server, Users, Settings, LogOut, Activity } from "lucide-react";
import { useAuth } from "../lib/auth";
import { cn } from "../lib/cn";

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutGrid, end: true },
  { to: "/hosts", label: "Hosts", icon: Server },
  { to: "/users", label: "Users", icon: Users, adminOnly: true },
  { to: "/settings", label: "Settings", icon: Settings, adminOnly: true },
];

export function Layout() {
  const { user, logout } = useAuth();

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 shrink-0 flex-col border-r border-[var(--border)] bg-[var(--surface-1)] px-3 py-4">
        <div className="mb-6 flex items-center gap-2 px-2">
          <Activity size={20} className="text-[var(--series-1)]" />
          <span className="text-sm font-semibold tracking-tight">Sentinel</span>
        </div>

        <nav className="flex flex-1 flex-col gap-0.5">
          {navItems
            .filter((item) => !item.adminOnly || user?.role === "admin")
            .map(({ to, label, icon: Icon, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-[var(--series-1)]/10 text-[var(--series-1)]"
                      : "text-[var(--text-secondary)] hover:bg-[var(--gridline)]/40 hover:text-[var(--text-primary)]",
                  )
                }
              >
                <Icon size={16} />
                {label}
              </NavLink>
            ))}
        </nav>

        <div className="border-t border-[var(--border)] pt-3">
          <div className="px-2.5 text-xs text-[var(--text-muted)]">
            {user?.username} · {user?.role}
          </div>
          <button
            onClick={() => logout()}
            className="mt-1 flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--gridline)]/40 hover:text-[var(--text-primary)]"
          >
            <LogOut size={16} />
            Log out
          </button>
        </div>
      </aside>

      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
