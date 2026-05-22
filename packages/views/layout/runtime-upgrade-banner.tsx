"use client";

import { useState } from "react";
import { AlertTriangle, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useMyRuntimesNeedUpdate } from "@multica/core/runtimes/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useNavigation } from "../navigation";
import { paths } from "@multica/core/paths";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useT } from "../i18n";

/**
 * Full-screen overlay shown when the current user has at least one local
 * runtime with an outdated CLI version. Dismissed for the session only —
 * reappears on the next page load until the runtime is upgraded.
 */
export function RuntimeUpgradeBanner() {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const needsUpdate = useMyRuntimesNeedUpdate(wsId);
  const [dismissed, setDismissed] = useState(false);
  const { push } = useNavigation();
  const workspace = useCurrentWorkspace();

  if (!needsUpdate || dismissed) return null;

  const handleGoToRuntimes = () => {
    if (workspace) {
      push(paths.workspace(workspace.slug).runtimes());
    }
    setDismissed(true);
  };

  return (
    <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="relative mx-4 w-full max-w-md rounded-xl border bg-card p-6 shadow-lg">
        <button
          type="button"
          onClick={() => setDismissed(true)}
          className="absolute right-3 top-3 rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:opacity-100"
          aria-label={t(($) => $.upgrade_banner.dismiss)}
        >
          <X className="h-4 w-4" />
        </button>

        <div className="flex flex-col items-center gap-4 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-warning/10">
            <AlertTriangle className="h-6 w-6 text-warning" />
          </div>

          <div className="space-y-1.5">
            <h2 className="text-base font-semibold">
              {t(($) => $.upgrade_banner.title)}
            </h2>
            <p className="text-sm text-muted-foreground">
              {t(($) => $.upgrade_banner.description)}
            </p>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setDismissed(true)}>
              {t(($) => $.upgrade_banner.dismiss)}
            </Button>
            <Button size="sm" onClick={handleGoToRuntimes}>
              {t(($) => $.upgrade_banner.go_to_runtimes)}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
