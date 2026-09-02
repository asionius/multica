"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { DiagnosticDetail } from "./diagnostic-detail";

export default function DiagnosticDetailRoutePage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const id = typeof params?.id === "string" ? params.id : "";
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      const next = id ? paths.diagnosticDetail(id) : paths.diagnostics();
      router.replace(`${paths.login()}?next=${encodeURIComponent(next)}`);
    }
  }, [isLoading, user, router, id]);

  if (isLoading || !user || !id) return null;

  return <DiagnosticDetail id={id} />;
}
