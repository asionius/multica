"use client";

import { useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { VerifyDevicePage } from "@multica/views/cli/verify-device-page";

/**
 * Next.js shell for the browser side of the CLI device-authorization flow.
 * Accepts ?code= prefilled from the CLI verification link. If the user
 * isn't signed in, bounce to /login with a redirect back here so the
 * code survives the round-trip.
 */
export default function CliVerifyPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const code = searchParams.get("code") ?? undefined;
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      const next = code
        ? `${paths.cliVerify()}?code=${encodeURIComponent(code)}`
        : paths.cliVerify();
      router.replace(`${paths.login()}?next=${encodeURIComponent(next)}`);
    }
  }, [isLoading, user, router, code]);

  if (isLoading || !user) return null;

  return <VerifyDevicePage initialCode={code} />;
}
