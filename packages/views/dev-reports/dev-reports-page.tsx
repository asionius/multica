"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileText, TriangleAlert } from "lucide-react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  devReportDetailOptions,
  devReportListOptions,
  devReportRevisionsOptions,
} from "@multica/core/dev-reports";
import { PageHeader } from "../layout/page-header";
import { useT } from "../i18n";
import { DevReportContent } from "./report-content";

/**
 * Workspace report hub: pick a version (newest observation first by default),
 * optionally a revision, and read it inline. The canonical reading URL for a
 * specific revision stays /{slug}/dev-report/{reportId}.
 */
export function DevReportsPage() {
  const { t } = useT("dev-reports");
  const wsId = useWorkspaceId();
  const [version, setVersion] = useState<string | null>(null);
  const [revisionId, setRevisionId] = useState<string | null>(null);

  const listQuery = useQuery(devReportListOptions(wsId));
  const reports = listQuery.data?.reports ?? [];
  const selectedVersionReport = reports.find((r) => r.version === version) ?? reports[0];

  const revisionsQuery = useQuery({
    ...devReportRevisionsOptions(wsId, selectedVersionReport?.id ?? ""),
    enabled: !!wsId && !!selectedVersionReport,
  });
  const revisions = revisionsQuery.data ?? [];
  const activeReport = revisions.find((r) => r.id === revisionId) ?? selectedVersionReport;

  const detailQuery = useQuery({
    ...devReportDetailOptions(wsId, activeReport?.id ?? ""),
    enabled: !!wsId && !!activeReport,
  });

  const selectors =
    reports.length > 0 ? (
      <div className="flex items-center gap-2 print:hidden">
        <Select
          value={selectedVersionReport?.version ?? ""}
          onValueChange={(next) => {
            setVersion(next ?? null);
            setRevisionId(null);
          }}
        >
          <SelectTrigger size="sm" className="min-w-[150px]">
            <SelectValue>
              {() => <span className="truncate">{selectedVersionReport?.version ?? ""}</span>}
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="start">
            {reports.map((report) => (
              <SelectItem key={report.version} value={report.version}>
                <span className="truncate">{report.version}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  {t(($) => $.revision_label, { revision: report.revision })}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {revisions.length > 1 ? (
          <Select
            value={activeReport?.id ?? ""}
            onValueChange={(next) => setRevisionId(next ?? null)}
          >
            <SelectTrigger size="sm" className="min-w-[120px]">
              <SelectValue>
                {() => (
                  <span className="truncate">
                    {t(($) => $.revision_label, { revision: activeReport?.revision ?? 0 })}
                  </span>
                )}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="start">
              {revisions.map((revision) => (
                <SelectItem key={revision.id} value={revision.id}>
                  {t(($) => $.revision_label, { revision: revision.revision })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
      </div>
    ) : null;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <div className="flex items-center gap-2 text-sm font-medium">
          <FileText className="size-4 text-muted-foreground" />
          {t(($) => $.title)}
        </div>
        <div className="ml-auto">{selectors}</div>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {listQuery.isLoading ? (
          <div className="mx-auto w-full max-w-[1320px] space-y-3 px-4 py-6 md:px-8">
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-48 w-full" />
            <Skeleton className="h-4 w-80" />
          </div>
        ) : listQuery.isError ? (
          <ReportErrorState onRetry={() => listQuery.refetch()} />
        ) : reports.length === 0 ? (
          <Empty className="mx-auto mt-16 max-w-md border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileText />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.empty.title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.empty.description)}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : detailQuery.isLoading ? (
          <div className="mx-auto w-full max-w-[1320px] space-y-3 px-4 py-6 md:px-8">
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-48 w-full" />
          </div>
        ) : detailQuery.isError || !detailQuery.data ? (
          <ReportErrorState onRetry={() => detailQuery.refetch()} />
        ) : (
          <DevReportContent report={detailQuery.data} />
        )}
      </div>
    </div>
  );
}

function ReportErrorState({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("dev-reports");
  return (
    <Empty className="mx-auto mt-16 max-w-md border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <TriangleAlert />
        </EmptyMedia>
        <EmptyTitle>{t(($) => $.error.title)}</EmptyTitle>
        <EmptyDescription>{t(($) => $.error.description)}</EmptyDescription>
        <Button size="sm" variant="outline" className="mt-2" onClick={onRetry}>
          {t(($) => $.error.retry)}
        </Button>
      </EmptyHeader>
    </Empty>
  );
}
