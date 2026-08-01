import type { ReactNode } from "react";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { AuthProvider, useAuth } from "./lib/auth";
import { Layout } from "./components/Layout";
import { Login } from "./pages/Login";
import { Dashboard } from "./pages/Dashboard";
import { Hosts } from "./pages/Hosts";
import { HostDetail } from "./pages/HostDetail";
import { LLMTargets } from "./pages/LLMTargets";
import { LLMEndpoints } from "./pages/LLMEndpoints";
import { LLMTargetDetail } from "./pages/LLMTargetDetail";
import { Users } from "./pages/Users";
import { Settings } from "./pages/Settings";

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center text-sm text-[var(--text-muted)]">
        Loading…
      </div>
    );
  }
  if (!user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  if (user?.role !== "admin") return <Navigate to="/" replace />;
  return <>{children}</>;
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="hosts" element={<Hosts />} />
        <Route path="hosts/:id" element={<HostDetail />} />
        <Route path="llm-endpoints" element={<LLMEndpoints />} />
        <Route path="llm-targets" element={<LLMTargets />} />
        <Route path="llm-targets/:id" element={<LLMTargetDetail />} />
        <Route
          path="users"
          element={
            <RequireAdmin>
              <Users />
            </RequireAdmin>
          }
        />
        <Route
          path="settings"
          element={
            <RequireAdmin>
              <Settings />
            </RequireAdmin>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  );
}
