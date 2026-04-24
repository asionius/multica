"use client";

import { useEffect, useRef, useState } from "react";
import {
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import { createQueryClient } from "./query-client";
import type { ReactNode } from "react";

// Module-level ref to the QueryClient created by QueryProvider. The auth
// store needs access at action time (smartGateLogin, verifyCode) to seed
// the workspace list cache *before* setting `user`, closing the race
// where subscribers redirect against an empty cache. Using a ref avoids
// coupling the store creation order to the React render order.
let _queryClient: QueryClient | null = null;

export function getRegisteredQueryClient(): QueryClient | null {
  return _queryClient;
}

function QueryClientRegistrar({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const ref = useRef<QueryClient | null>(null);
  if (ref.current !== qc) {
    ref.current = qc;
    _queryClient = qc;
  }
  useEffect(() => () => {
    if (_queryClient === qc) _queryClient = null;
  }, [qc]);
  return <>{children}</>;
}

export function QueryProvider({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  return (
    <QueryClientProvider client={queryClient}>
      <QueryClientRegistrar>{children}</QueryClientRegistrar>
    </QueryClientProvider>
  );
}
