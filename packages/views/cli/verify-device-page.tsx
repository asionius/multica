"use client";

import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { CliDeviceVerifyResponse } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Check, Monitor, X } from "lucide-react";

// Local string table — pulled inline (rather than via the @multica/views
// react-i18next setup) so this page keeps working in deployments that
// haven't wired up the runtime translations resource yet. English-only
// for now; promote to i18next selectors once the views package finishes
// migrating namespaces.
const cliVerifyStrings = {
  title: "Authorize CLI device",
  body: "Enter the code shown in your terminal, or review the request below if it was filled in automatically.",
  codeLabel: "Device code",
  codePlaceholder: "XXXX-XXXX",
  continue: "Continue",
  verifying: "Verifying...",
  confirmTitle: "Authorize this device?",
  confirmBody:
    "A CLI on {hostname} wants to sign in as you. Only approve if you started this login.",
  requestedAt: "Requested {date}",
  expiresAt: "Expires {date}",
  authorize: "Authorize",
  authorizing: "Authorizing...",
  deny: "This wasn't me",
  denying: "Denying...",
  approvedTitle: "Device authorized",
  approvedBody: "You can close this tab and return to your terminal.",
  deniedTitle: "Authorization denied",
  deniedBody: "The CLI request was denied. You can close this tab.",
  errorInvalidCode: "That code is not valid or has already been used.",
  errorExpired: "That code has expired — run `multica login` again.",
  errorGeneric: "Something went wrong. Please try again.",
} as const;

type TFn = (
  key: string,
  vars?: Record<string, string | number>,
) => string;

const t: TFn = (key, vars) => {
  const leaf = key.replace(/^cli\.verify\./, "");
  const template =
    cliVerifyStrings[leaf as keyof typeof cliVerifyStrings] ?? key;
  if (!vars) return template;
  return template.replace(/\{(\w+)\}/g, (m, name) =>
    name in vars ? String(vars[name]) : m,
  );
};

/**
 * Browser-side of the RFC 8628-style device flow. The CLI prints a
 * short user code; the user lands here (via the link we print, or by
 * pasting the code manually) and clicks Authorize or Deny.
 *
 * Why this is its own page (not a modal or settings tab):
 *  - It must be reachable even if the user has no workspace yet, so it
 *    lives under /cli/ (global prefix), not inside a workspace scope.
 *  - The Next.js shell handles auth-redirect; this component assumes
 *    the user is signed in (the shell returns null otherwise).
 *
 * Flow states:
 *  input          — 8-slot manual entry (when no ?code= was supplied)
 *  verifying      — after Continue, before the server confirms the code
 *  confirm        — server returned hostname + timestamps; show buttons
 *  approving      — Authorize clicked, waiting on /approve
 *  approved       — terminal success state; user can close the tab
 *  denying        — Deny clicked, waiting on /deny
 *  denied         — terminal rejection state
 *  error          — verify/approve/deny failed; message below the form
 *
 * The /verify call returns 404/410 for invalid/expired codes. We map
 * those to user-friendly messages; network errors bubble through the
 * generic fallback.
 */

type Phase =
  | "input"
  | "verifying"
  | "confirm"
  | "approving"
  | "approved"
  | "denying"
  | "denied";

interface VerifyDevicePageProps {
  /** Prefilled user_code from the CLI's verification link (?code=). */
  initialCode?: string;
}

export function VerifyDevicePage({ initialCode }: VerifyDevicePageProps) {
  const [phase, setPhase] = useState<Phase>("input");
  const [code, setCode] = useState(initialCode?.trim() ?? "");
  const [verified, setVerified] = useState<CliDeviceVerifyResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Auto-verify a prefilled code once on mount so users who followed the
  // link from the CLI don't have to click Continue for a code they never typed.
  const autoRan = useRef(false);
  useEffect(() => {
    if (autoRan.current) return;
    autoRan.current = true;
    if (initialCode && initialCode.trim().length > 0) {
      void verify(initialCode);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function verify(userCode: string) {
    setPhase("verifying");
    setError(null);
    try {
      const result = await api.verifyCliDevice(userCode);
      setVerified(result);
      setCode(userCode);
      setPhase("confirm");
    } catch (e) {
      setPhase("input");
      setError(mapError(e, t));
    }
  }

  async function approve() {
    if (!verified) return;
    setPhase("approving");
    setError(null);
    try {
      await api.approveCliDevice(code);
      setPhase("approved");
    } catch (e) {
      setPhase("confirm");
      const msg = mapError(e, t);
      setError(msg);
      toast.error(msg);
    }
  }

  async function deny() {
    if (!verified) return;
    setPhase("denying");
    setError(null);
    try {
      await api.denyCliDevice(code);
      setPhase("denied");
    } catch (e) {
      setPhase("confirm");
      const msg = mapError(e, t);
      setError(msg);
      toast.error(msg);
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-6 py-12">
      <Card className="w-full max-w-md">
        <CardContent className="flex flex-col items-center gap-6 py-10">
          {phase === "approved" ? (
            <>
              <Icon tone="success">
                <Check className="h-6 w-6" />
              </Icon>
              <Heading>{t("cli.verify.approvedTitle")}</Heading>
              <Body>{t("cli.verify.approvedBody")}</Body>
            </>
          ) : phase === "denied" ? (
            <>
              <Icon tone="muted">
                <X className="h-6 w-6" />
              </Icon>
              <Heading>{t("cli.verify.deniedTitle")}</Heading>
              <Body>{t("cli.verify.deniedBody")}</Body>
            </>
          ) : phase === "confirm" || phase === "approving" || phase === "denying" ? (
            <ConfirmView
              t={t}
              verified={verified!}
              onApprove={approve}
              onDeny={deny}
              busy={phase}
              error={error}
            />
          ) : (
            <InputView
              t={t}
              code={code}
              setCode={setCode}
              onContinue={() => verify(code)}
              busy={phase === "verifying"}
              error={error}
            />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function InputView({
  t,
  code,
  setCode,
  onContinue,
  busy,
  error,
}: {
  t: TFn;
  code: string;
  setCode: (v: string) => void;
  onContinue: () => void;
  busy: boolean;
  error: string | null;
}) {
  return (
    <>
      <Icon tone="primary">
        <Monitor className="h-6 w-6" />
      </Icon>
      <Heading>{t("cli.verify.title")}</Heading>
      <Body>{t("cli.verify.body")}</Body>
      <form
        className="flex w-full flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy && code.trim().length > 0) onContinue();
        }}
      >
        <label className="text-sm font-medium" htmlFor="cli-code">
          {t("cli.verify.codeLabel")}
        </label>
        <Input
          id="cli-code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder={t("cli.verify.codePlaceholder")}
          autoFocus
          autoComplete="off"
          autoCapitalize="characters"
          spellCheck={false}
          className="text-center font-mono tracking-widest uppercase"
        />
        <Button type="submit" disabled={busy || code.trim().length === 0}>
          {busy ? t("cli.verify.verifying") : t("cli.verify.continue")}
        </Button>
        {error && (
          <p className="text-center text-sm text-destructive">{error}</p>
        )}
      </form>
    </>
  );
}

function ConfirmView({
  t,
  verified,
  onApprove,
  onDeny,
  busy,
  error,
}: {
  t: TFn;
  verified: CliDeviceVerifyResponse;
  onApprove: () => void;
  onDeny: () => void;
  busy: Phase;
  error: string | null;
}) {
  const approving = busy === "approving";
  const denying = busy === "denying";
  const disabled = approving || denying;
  return (
    <>
      <Icon tone="primary">
        <Monitor className="h-6 w-6" />
      </Icon>
      <Heading>{t("cli.verify.confirmTitle")}</Heading>
      <Body>
        {t("cli.verify.confirmBody", { hostname: verified.hostname })}
      </Body>
      <div className="flex w-full flex-col gap-1 text-sm text-muted-foreground">
        <div>
          {t("cli.verify.requestedAt", {
            date: new Date(verified.requested_at).toLocaleString(),
          })}
        </div>
        <div>
          {t("cli.verify.expiresAt", {
            date: new Date(verified.expires_at).toLocaleString(),
          })}
        </div>
      </div>
      <div className="flex w-full gap-3">
        <Button
          variant="outline"
          className="flex-1"
          onClick={onDeny}
          disabled={disabled}
        >
          {denying ? t("cli.verify.denying") : t("cli.verify.deny")}
        </Button>
        <Button
          className="flex-1"
          onClick={onApprove}
          disabled={disabled}
        >
          {approving ? t("cli.verify.authorizing") : t("cli.verify.authorize")}
        </Button>
      </div>
      {error && (
        <p className="text-center text-sm text-destructive">{error}</p>
      )}
    </>
  );
}

function Heading({ children }: { children: React.ReactNode }) {
  return <h2 className="text-xl font-semibold">{children}</h2>;
}

function Body({ children }: { children: React.ReactNode }) {
  return <p className="text-center text-sm text-muted-foreground">{children}</p>;
}

function Icon({
  tone,
  children,
}: {
  tone: "primary" | "success" | "muted";
  children: React.ReactNode;
}) {
  const cls =
    tone === "success"
      ? "bg-primary/10 text-primary"
      : tone === "muted"
      ? "bg-muted text-muted-foreground"
      : "bg-primary/10 text-primary";
  return (
    <div className={`flex h-14 w-14 items-center justify-center rounded-full ${cls}`}>
      {children}
    </div>
  );
}

// The server returns 404 for unknown codes, 410 for expired/resolved,
// and 401 for unauthenticated. The auth shell redirects before we hit
// 401, so here we only distinguish invalid vs expired vs other.
function mapError(
  e: unknown,
  t: TFn,
): string {
  if (e instanceof Error) {
    const msg = e.message.toLowerCase();
    if (msg.includes("expired") || msg.includes("410")) {
      return t("cli.verify.errorExpired");
    }
    if (msg.includes("404") || msg.includes("invalid")) {
      return t("cli.verify.errorInvalidCode");
    }
  }
  return t("cli.verify.errorGeneric");
}
