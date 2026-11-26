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
import {
  publicDevReportDetailOptions,
  publicDevReportListOptions,
  publicDevReportRevisionsOptions,
} from "@multica/core/dev-reports";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useT } from "../i18n";
import { DevReportContent } from "./report-content";

/**
 * Public reading page (no login, no workspace membership). Same content as the
 * member page, but rendered standalone with the unauthenticated read API.
 */
export function DevReportsPublicPage({ slug }: { slug: string }) {
  const { t } = useT("dev-reports");
  const hasSession = !!useAuthStore((s) => s.user);
  const [version, setVersion] = useState<string | null>(null);
  const [revisionId, setRevisionId] = useState<string | null>(null);

  const listQuery = useQuery(publicDevReportListOptions(slug));
  const reports = listQuery.data?.reports ?? [];
  const selectedVersionReport = reports.find((r) => r.version === version) ?? reports[0];

  const revisionsQuery = useQuery({
    ...publicDevReportRevisionsOptions(slug, selectedVersionReport?.id ?? ""),
    enabled: !!selectedVersionReport,
  });
  const revisions = revisionsQuery.data ?? [];
  const activeReport = revisions.find((r) => r.id === revisionId) ?? selectedVersionReport;

  const detailQuery = useQuery({
    ...publicDevReportDetailOptions(slug, activeReport?.id ?? ""),
    enabled: !!activeReport,
  });

  return (
    // The web app's <body> is overflow-hidden (the dashboard owns its own
    // scroll panes), so this standalone page must provide the scroll container
    // itself or the wheel has nowhere to go.
    <div className="h-svh overflow-y-auto bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex min-h-12 max-w-[1320px] flex-wrap items-center gap-3 px-4 py-2 md:px-8">
          <FileText className="size-4 text-muted-foreground" />
          <span className="text-sm font-medium">{t(($) => $.title)}</span>
          {hasSession ? (
            <a
              href={`/${encodeURIComponent(slug)}/issues`}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              {t(($) => $.back_to_workspace)}
            </a>
          ) : null}
          {reports.length > 0 ? (
            <div className="ml-auto flex items-center gap-2 print:hidden">
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
                <SelectContent align="end">
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
                  <SelectContent align="end">
                    {revisions.map((revision) => (
                      <SelectItem key={revision.id} value={revision.id}>
                        {t(($) => $.revision_label, { revision: revision.revision })}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
            </div>
          ) : null}
        </div>
      </header>

      <main>
        {listQuery.isLoading || detailQuery.isLoading ? (
          <div className="mx-auto w-full max-w-[1320px] space-y-3 px-4 py-6 md:px-8">
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-48 w-full" />
          </div>
        ) : listQuery.isError || detailQuery.isError || !detailQuery.data ? (
          <Empty className="mx-auto mt-16 max-w-md border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <TriangleAlert />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.error.title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.error.description)}</EmptyDescription>
              <Button
                size="sm"
                variant="outline"
                className="mt-2"
                onClick={() => {
                  listQuery.refetch();
                  detailQuery.refetch();
                }}
              >
                {t(($) => $.error.retry)}
              </Button>
            </EmptyHeader>
          </Empty>
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
        ) : (
          <DevReportContent
            report={detailQuery.data}
            fetchMarkdown={(id) => api.getPublicDevReportMarkdown(slug, id)}
          />
        )}
      </main>
    </div>
  );
}
