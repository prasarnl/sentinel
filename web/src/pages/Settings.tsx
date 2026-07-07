import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";

export function Settings() {
  const [retentionDays, setRetentionDays] = useState(90);
  const [saved, setSaved] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    api.getSettings().then((s) => setRetentionDays(Number(s.retention_days)));
  }, []);

  async function onSave() {
    setSaving(true);
    setMessage(null);
    try {
      await api.updateSettings(retentionDays);
      setSaved(true);
      setMessage("Retention policy updated.");
    } catch {
      setMessage("Failed to update retention policy.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <h1 className="mb-4 text-lg font-semibold">Settings</h1>
      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>Data retention</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="mb-4 text-sm text-[var(--text-secondary)]">
            Metrics older than this are automatically dropped. Choose between 30 and 365 days.
          </p>
          <div className="mb-2 flex items-center gap-4">
            <input
              type="range"
              min={30}
              max={365}
              step={1}
              value={retentionDays}
              onChange={(e) => {
                setRetentionDays(Number(e.target.value));
                setSaved(false);
              }}
              className="flex-1 accent-[var(--series-1)]"
            />
            <span className="w-24 shrink-0 tabular-nums text-sm font-medium">{retentionDays} days</span>
          </div>
          {message && (
            <p className={saved ? "text-xs text-[var(--status-good)]" : "text-xs text-[var(--status-critical)]"}>
              {message}
            </p>
          )}
          <div className="mt-4">
            <Button onClick={onSave} disabled={saving || saved}>
              {saving ? "Saving…" : "Save changes"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
