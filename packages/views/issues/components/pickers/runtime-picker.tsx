"use client";

import { useState, useMemo } from "react";
import { Server } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { UpdateIssueRequest, AgentRuntime } from "@multica/core/types";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useAuthStore, type AuthState } from "@multica/core/auth";
import { useT } from "../../../i18n";
import { PropertyPicker, PickerItem, PickerSection, PickerEmpty } from "./property-picker";

// RuntimePicker — pin the issue to a specific agent_runtime so the daemon
// dispatches the assigned agent on this runtime instead of agent.runtime_id.
// Permission rule mirrors the server (canUseRuntimeForAgent): a workspace
// member sees their own private runtimes + every public runtime in the
// workspace; private runtimes owned by someone else are hidden (the server
// would 403 anyway). Workspace owner/admin sees everything.
//
// Designed to be rendered only when the issue's assignee is an agent — for
// member/squad assignees runtime has no effect, so the parent should hide
// this picker entirely.
export function RuntimePicker({
  workspaceId,
  runtimeId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  align = "start",
  defaultOpen = false,
}: {
  workspaceId: string;
  runtimeId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);

  // Auth store user id is the source-of-truth for "is this private runtime
  // mine?" — selectors return primitives so the equality stays cheap.
  const currentUserId = useAuthStore(
    (s: AuthState) => s.user?.id ?? null,
  );

  const { data: runtimes = [], isLoading } = useQuery({
    ...runtimeListOptions(workspaceId),
    enabled: open && !!workspaceId,
  });

  // Filter to "I can use": workspace owner/admin already sees everything via
  // server permission, but the client doesn't know the role. The conservative
  // client-side filter shows my runtimes + public runtimes. Anything else
  // would 403 on submit, so hiding here avoids a confusing UX. The server
  // remains authoritative.
  const { mine, shared } = useMemo(() => {
    const mine: AgentRuntime[] = [];
    const shared: AgentRuntime[] = [];
    for (const rt of runtimes) {
      if (rt.owner_id && rt.owner_id === currentUserId) {
        mine.push(rt);
      } else if (rt.visibility === "public") {
        shared.push(rt);
      }
    }
    return { mine, shared };
  }, [runtimes, currentUserId]);

  const selected = runtimes.find((rt) => rt.id === runtimeId) ?? null;
  const triggerLabel = selected ? selected.name : t(($) => $.pickers.runtime.trigger_label);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-72"
      align={align}
      trigger={
        customTrigger ?? (
          <>
            <Server className="h-3.5 w-3.5 text-muted-foreground" />
            <span className={selected ? "" : "text-muted-foreground"}>
              {triggerLabel}
            </span>
          </>
        )
      }
      triggerRender={triggerRender}
    >
      {isLoading && (
        <div className="px-2 py-3 text-center text-sm text-muted-foreground">
          {t(($) => $.pickers.runtime.loading)}
        </div>
      )}
      {!isLoading && mine.length === 0 && shared.length === 0 && <PickerEmpty />}
      {!isLoading && mine.length > 0 && (
        <PickerSection label={t(($) => $.pickers.runtime.section_mine)}>
          {mine.map((rt) => (
            <PickerItem
              key={rt.id}
              selected={rt.id === runtimeId}
              onClick={() => {
                onUpdate({ runtime_id: rt.id });
                setOpen(false);
              }}
              tooltip={`${rt.name} · ${rt.visibility}`}
            >
              <Server className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{rt.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}
      {!isLoading && shared.length > 0 && (
        <PickerSection label={t(($) => $.pickers.runtime.section_shared)}>
          {shared.map((rt) => (
            <PickerItem
              key={rt.id}
              selected={rt.id === runtimeId}
              onClick={() => {
                onUpdate({ runtime_id: rt.id });
                setOpen(false);
              }}
              tooltip={`${rt.name} · public`}
            >
              <Server className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{rt.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}
      {selected && (
        <PickerSection label={t(($) => $.pickers.runtime.section_pin)}>
          <PickerItem
            selected={false}
            onClick={() => {
              onUpdate({ runtime_id: null });
              setOpen(false);
            }}
          >
            <span className="text-muted-foreground">
              {t(($) => $.pickers.runtime.clear_action)}
            </span>
          </PickerItem>
        </PickerSection>
      )}
    </PropertyPicker>
  );
}
