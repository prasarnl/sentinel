import { useEffect, useState, type FormEvent } from "react";
import { Plus, Trash2, KeyRound } from "lucide-react";
import { api, type User, type Role } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";

function AddUserDialog({ onClose }: { onClose: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createUser(username, password, role);
      onClose();
    } catch {
      setError("Failed to create user — username may already be taken");
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Add user</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Username</label>
              <Input autoFocus value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Password</label>
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Role</label>
              <Select value={role} onChange={(e) => setRole(e.target.value as Role)} className="w-full">
                <option value="viewer">Viewer (read-only)</option>
                <option value="admin">Admin</option>
              </Select>
            </div>
            {error && <div className="text-xs text-[var(--status-critical)]">{error}</div>}
            <div className="mt-2 flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={!username || !password}>
                Create user
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

function ResetPasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await api.updateUser(user.id, { password });
      onClose();
    } catch {
      setError("Failed to update password");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Set new password for {user.username}</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">New password</label>
              <Input
                autoFocus
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
              />
            </div>
            {error && <div className="text-xs text-[var(--status-critical)]">{error}</div>}
            <div className="mt-2 flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={!password || saving}>
                {saving ? "Saving…" : "Update password"}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export function Users() {
  const { user: currentUser } = useAuth();
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [resetTarget, setResetTarget] = useState<User | null>(null);

  function refresh() {
    api
      .listUsers()
      .then(setUsers)
      .finally(() => setLoading(false));
  }

  useEffect(refresh, []);

  async function onRoleChange(u: User, role: Role) {
    await api.updateUser(u.id, { role });
    refresh();
  }

  async function onDelete(u: User) {
    if (!confirm(`Remove user "${u.username}"?`)) return;
    await api.deleteUser(u.id);
    refresh();
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">Users</h1>
        <Button onClick={() => setShowAdd(true)}>
          <Plus size={16} /> Add user
        </Button>
      </div>

      <Card>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-xs text-[var(--text-muted)]">
              <th className="px-4 py-2.5 font-medium">Username</th>
              <th className="px-4 py-2.5 font-medium">Role</th>
              <th className="px-4 py-2.5 font-medium">Created</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {!loading &&
              users.map((u) => (
                <tr key={u.id} className="border-b border-[var(--border)] last:border-0">
                  <td className="px-4 py-2.5 font-medium">{u.username}</td>
                  <td className="px-4 py-2.5">
                    <Select
                      value={u.role}
                      disabled={u.id === currentUser?.id}
                      onChange={(e) => onRoleChange(u, e.target.value as Role)}
                    >
                      <option value="viewer">Viewer</option>
                      <option value="admin">Admin</option>
                    </Select>
                  </td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">
                    {new Date(u.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex items-center justify-end gap-3">
                      <button
                        onClick={() => setResetTarget(u)}
                        title="Set new password"
                        className="text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                      >
                        <KeyRound size={14} />
                      </button>
                      {u.id !== currentUser?.id && (
                        <button
                          onClick={() => onDelete(u)}
                          title="Remove user"
                          className="text-[var(--text-muted)] hover:text-[var(--status-critical)]"
                        >
                          <Trash2 size={14} />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </Card>

      {showAdd && (
        <AddUserDialog
          onClose={() => {
            setShowAdd(false);
            refresh();
          }}
        />
      )}

      {resetTarget && <ResetPasswordDialog user={resetTarget} onClose={() => setResetTarget(null)} />}
    </div>
  );
}
