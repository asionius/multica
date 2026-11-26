"use client";

import { use } from "react";
import { DevReportPublicPage } from "@multica/views/dev-reports";

export default function Page({
  params,
}: {
  params: Promise<{ workspaceSlug: string; reportId: string }>;
}) {
  const { workspaceSlug, reportId } = use(params);
  return <DevReportPublicPage slug={workspaceSlug} reportId={reportId} />;
}
