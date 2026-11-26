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
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  devReportDetailOptions,
  devReportRevisionsOptions,
} from "@multica/core/dev-reports";
import { AppLink, useNavigation } from "../navigation";
import { PageHeader } from "../layout/page-header";
import { useT } from "../i18n";
import { DevReportContent } from "./report-content";

/**
 * Stable reading URL for one immutable revision. Changing the revision
 * navigates to that revision's own URL rather than mutating this page, so a
 * shared link always resolves to the same content.
 */
export function DevReportPage({ reportId }: { reportId: string }) {
  const { t } = useT("dev-reports");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const detailQuery = useQuery(devReportDetailOptions(wsId, reportId));
  const revisionsQuery = useQuery({
    ...devReportRevisionsOptions(wsId, reportId),
    enabled: !!wsId && !!detailQuery.data,
  });
  const revisions = revisionsQuery.data ?? [];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <Button
          size="sm"
          variant="ghost"
          className="print:hidden"
          render={<AppLink href={paths.devReports()} />}
        >
          <ArrowLeft className="size-3.5" />
          {t(($) => $.back_to_list)}
        </Button>
        <div className="ml-2 hidden items-center gap-2 text-sm font-medium sm:flex">
          <FileText className="size-4 text-muted-foreground" />
          {t(($) => $.title)}
        </div>
        {revisions.length > 1 ? (
          <div className="ml-auto print:hidden">
            <Select
              value={reportId}
              onValueChange={(next) => {
                if (next) navigation.push(paths.devReportDetail(next));
              }}
            >
              <SelectTrigger size="sm" className="min-w-[120px]">
                <SelectValue>
                  {() => (
                    <span className="truncate">
                      {t(($) => $.revision_label, {
                        revision: detailQuery.data?.revision ?? 0,
                      })}
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
          </div>
        ) : null}
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
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
          <DevReportContent report={detailQuery.data} />
        )}
      </div>
    </div>
  );
}
