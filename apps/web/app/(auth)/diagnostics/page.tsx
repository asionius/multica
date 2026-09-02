"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { DiagnosticsList } from "./diagnostics-list";

export default function DiagnosticsRoutePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(
        `${paths.login()}?next=${encodeURIComponent(paths.diagnostics())}`,
      );
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return <DiagnosticsList />;
}
