"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, FileText, TriangleAlert } from "lucide-react";
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
import { api } from "@multica/core/api";
import {
  publicDevReportDetailOptions,
  publicDevReportRevisionsOptions,
} from "@multica/core/dev-reports";
import { useT } from "../i18n";
import { DevReportContent } from "./report-content";

/**
 * Public stable URL for one immutable revision. Shareable without a session;
 * the revision picker performs a document navigation so the address bar always
 * points at the revision being read.
 */
export function DevReportPublicPage({ slug, reportId }: { slug: string; reportId: string }) {
  const { t } = useT("dev-reports");

  const detailQuery = useQuery(publicDevReportDetailOptions(slug, reportId));
  const revisionsQuery = useQuery({
    ...publicDevReportRevisionsOptions(slug, reportId),
    enabled: !!detailQuery.data,
  });
  const revisions = revisionsQuery.data ?? [];

  return (
    <div className="h-svh overflow-y-auto bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex min-h-12 max-w-[1320px] flex-wrap items-center gap-3 px-4 py-2 md:px-8">
          <a
            href={`/${encodeURIComponent(slug)}/dev-report`}
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-3.5" />
            {t(($) => $.back_to_list)}
          </a>
          <span className="hidden items-center gap-2 text-sm font-medium sm:flex">
            <FileText className="size-4 text-muted-foreground" />
            {t(($) => $.title)}
          </span>
          {revisions.length > 1 ? (
            <div className="ml-auto print:hidden">
              <Select
                value={reportId}
                onValueChange={(next) => {
                  if (next) {
                    window.location.assign(
                      `/${encodeURIComponent(slug)}/dev-report/${encodeURIComponent(next)}`,
                    );
                  }
                }}
              >
                <SelectTrigger size="sm" className="min-w-[120px]">
                  <SelectValue>
                    {() => (
                      <span className="truncate">
                        {t(($) => $.revision_label, { revision: detailQuery.data?.revision ?? 0 })}
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
            </div>
          ) : null}
        </div>
      </header>

      <main>
        {detailQuery.isLoading ? (
          <div className="mx-auto w-full max-w-[1320px] space-y-3 px-4 py-6 md:px-8">
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-48 w-full" />
          </div>
        ) : detailQuery.isError || !detailQuery.data ? (
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
                onClick={() => detailQuery.refetch()}
              >
                {t(($) => $.error.retry)}
              </Button>
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
