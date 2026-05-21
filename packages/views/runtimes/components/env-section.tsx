"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Eye,
  EyeOff,
  Loader2,
  Lock,
  Plus,
  Save,
  Trash2,
} from "lucide-react";
import type { AgentRuntime } from "@multica/core/types";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import { useT } from "../../i18n";

// EnvSection renders the per-runtime custom_env editor on the runtime detail
// page. Mirrors agents/components/tabs/env-tab.tsx — same UX (key/value rows,
// password mask + reveal, duplicate-key guard) but writes to the runtime via
// PATCH /api/runtimes/:id and is gated by `canEdit` (= owner or workspace
// admin). For non-editors the server returns redacted values ("****" + the
// `custom_env_redacted` flag) so we render a read-only locked view.
//
// At dispatch the daemon merges agent.custom_env first then runtime.custom_env
// (runtime wins); see daemon.go and the merge comment in handler/daemon.go.

let nextEnvId = 0;

interface EnvEntry {
  id: number;
  key: string;
  value: string;
  visible: boolean;
}

function envMapToEntries(env: Record<string, string>): EnvEntry[] {
  return Object.entries(env).map(([key, value]) => ({
    id: nextEnvId++,
    key,
    value,
    visible: false,
  }));
}

function entriesToEnvMap(entries: EnvEntry[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) {
      map[key] = entry.value;
    }
  }
  return map;
}

export function EnvSection({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const updateRuntime = useUpdateRuntime(wsId);

  const originalEnvMap = useMemo(
    () => runtime.custom_env ?? {},
    [runtime.custom_env],
  );
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>(() =>
    envMapToEntries(originalEnvMap),
  );
  const [saving, setSaving] = useState(false);

  // Reset local state when the upstream runtime changes (cache refetch after
  // save, or user navigated to a different runtime via list).
  useEffect(() => {
    setEnvEntries(envMapToEntries(originalEnvMap));
  }, [originalEnvMap]);

  const currentEnvMap = entriesToEnvMap(envEntries);
  const dirty =
    JSON.stringify(currentEnvMap) !== JSON.stringify(originalEnvMap);

  const redacted = runtime.custom_env_redacted === true;
  const readOnly = !canEdit || redacted;

  const addEnvEntry = () => {
    setEnvEntries([
      ...envEntries,
      { id: nextEnvId++, key: "", value: "", visible: true },
    ]);
  };
  const removeEnvEntry = (index: number) => {
    setEnvEntries(envEntries.filter((_, i) => i !== index));
  };
  const updateEnvEntry = (
    index: number,
    field: "key" | "value",
    val: string,
  ) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, [field]: val } : entry,
      ),
    );
  };
  const toggleEnvVisibility = (index: number) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, visible: !entry.visible } : entry,
      ),
    );
  };

  const handleSave = async () => {
    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    const uniqueKeys = new Set(keys);
    if (uniqueKeys.size < keys.length) {
      toast.error(t(($) => $.detail.env.duplicate_keys_toast));
      return;
    }
    setSaving(true);
    try {
      await updateRuntime.mutateAsync({
        runtimeId: runtime.id,
        patch: { custom_env: currentEnvMap },
      });
      toast.success(t(($) => $.detail.env.saved_toast));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.detail.env.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">
          {t(($) => $.detail.env.title)}
        </h2>
        {!readOnly && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addEnvEntry}
            className="shrink-0"
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.detail.env.add)}
          </Button>
        )}
      </div>

      <p className="mb-3 text-xs text-muted-foreground">
        {readOnly && redacted
          ? t(($) => $.detail.env.intro_readonly)
          : t(($) => $.detail.env.intro)}
      </p>

      {envEntries.length === 0 && (
        <p className="text-xs italic text-muted-foreground">
          {readOnly
            ? t(($) => $.detail.env.empty_readonly)
            : t(($) => $.detail.env.empty)}
        </p>
      )}

      {envEntries.length > 0 && (
        <div className="space-y-2">
          {envEntries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.key}
                readOnly={readOnly}
                onChange={
                  readOnly
                    ? undefined
                    : (e) => updateEnvEntry(index, "key", e.target.value)
                }
                placeholder={t(($) => $.detail.env.key_placeholder)}
                className={`w-[40%] font-mono text-xs ${readOnly ? "bg-muted" : ""}`}
              />
              <div className="relative flex-1">
                <Input
                  type={!readOnly && entry.visible ? "text" : "password"}
                  value={readOnly && redacted ? "****" : entry.value}
                  readOnly={readOnly}
                  onChange={
                    readOnly
                      ? undefined
                      : (e) => updateEnvEntry(index, "value", e.target.value)
                  }
                  placeholder={t(($) => $.detail.env.value_placeholder)}
                  className={`pr-8 font-mono text-xs ${readOnly ? "bg-muted" : ""}`}
                />
                {readOnly ? (
                  <Lock className="absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                ) : (
                  <button
                    type="button"
                    onClick={() => toggleEnvVisibility(index)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    aria-label={
                      entry.visible
                        ? t(($) => $.detail.env.hide_value_aria)
                        : t(($) => $.detail.env.show_value_aria)
                    }
                  >
                    {entry.visible ? (
                      <EyeOff className="h-3.5 w-3.5" />
                    ) : (
                      <Eye className="h-3.5 w-3.5" />
                    )}
                  </button>
                )}
              </div>
              {!readOnly && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => removeEnvEntry(index)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label={t(($) => $.detail.env.remove_aria)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          ))}
        </div>
      )}

      {!readOnly && (
        <div className="mt-3 flex items-center justify-end gap-3">
          {dirty && (
            <span className="text-xs text-muted-foreground">
              {t(($) => $.detail.env.unsaved_changes)}
            </span>
          )}
          <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
            {saving ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            {t(($) => $.detail.env.save)}
          </Button>
        </div>
      )}
    </section>
  );
}
