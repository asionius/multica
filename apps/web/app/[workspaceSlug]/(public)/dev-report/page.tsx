"use client";

import { use } from "react";
import { DevReportsPublicPage } from "@multica/views/dev-reports";

export default function Page({ params }: { params: Promise<{ workspaceSlug: string }> }) {
  const { workspaceSlug } = use(params);
  return <DevReportsPublicPage slug={workspaceSlug} />;
}
