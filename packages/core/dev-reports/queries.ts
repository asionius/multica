import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// All report queries are workspace-scoped: the workspace id is part of the
// key so switching workspaces never shows another workspace's reports.
export const devReportKeys = {
  all: (wsId: string) => ["dev-reports", wsId] as const,
  /** Prefix that matches every filtered list for this workspace. */
  lists: (wsId: string) => [...devReportKeys.all(wsId), "list"] as const,
  list: (wsId: string, version?: string) =>
    [...devReportKeys.lists(wsId), version ?? "all"] as const,
  detail: (wsId: string, reportId: string) =>
    [...devReportKeys.all(wsId), "detail", reportId] as const,
  revisions: (wsId: string, reportId: string) =>
    [...devReportKeys.all(wsId), "revisions", reportId] as const,
};

export function devReportListOptions(wsId: string, version?: string) {
  return queryOptions({
    queryKey: devReportKeys.list(wsId, version),
    queryFn: () =>
      api.listDevReports({ version, limit: 100 }),
    enabled: !!wsId,
  });
}

/**
 * A single immutable revision. Throws when the payload is unreadable so the
 * page shows an explicit error state instead of an empty report.
 */
export function devReportDetailOptions(wsId: string, reportId: string) {
  return queryOptions({
    queryKey: devReportKeys.detail(wsId, reportId),
    queryFn: async () => {
      const report = await api.getDevReport(reportId);
      if (!report) throw new Error("dev report unavailable");
      return report;
    },
    enabled: !!wsId && !!reportId,
  });
}

/**
 * All revisions of the version that `reportId` belongs to, newest first.
 * Used by the revision picker on both the list and detail pages.
 */
export function devReportRevisionsOptions(wsId: string, reportId: string) {
  return queryOptions({
    queryKey: devReportKeys.revisions(wsId, reportId),
    queryFn: () => api.listDevReportRevisions(reportId),
    select: (data) => data.revisions,
    enabled: !!wsId && !!reportId,
  });
}


// Public (no workspace membership required) read paths. The workspace slug is
// part of the key so two workspaces with the same version never collide.
export const publicDevReportKeys = {
  all: (workspaceSlug: string) => ["dev-reports", "public", workspaceSlug] as const,
  lists: (workspaceSlug: string) => [...publicDevReportKeys.all(workspaceSlug), "list"] as const,
  list: (workspaceSlug: string, version?: string) =>
    [...publicDevReportKeys.lists(workspaceSlug), version ?? "all"] as const,
  detail: (workspaceSlug: string, reportId: string) =>
    [...publicDevReportKeys.all(workspaceSlug), "detail", reportId] as const,
  revisions: (workspaceSlug: string, reportId: string) =>
    [...publicDevReportKeys.all(workspaceSlug), "revisions", reportId] as const,
};

export function publicDevReportListOptions(workspaceSlug: string, version?: string) {
  return queryOptions({
    queryKey: publicDevReportKeys.list(workspaceSlug, version),
    queryFn: () => api.listPublicDevReports(workspaceSlug, { version, limit: 100 }),
    enabled: !!workspaceSlug,
  });
}

export function publicDevReportDetailOptions(workspaceSlug: string, reportId: string) {
  return queryOptions({
    queryKey: publicDevReportKeys.detail(workspaceSlug, reportId),
    queryFn: async () => {
      const report = await api.getPublicDevReport(workspaceSlug, reportId);
      if (!report) throw new Error("dev report unavailable");
      return report;
    },
    enabled: !!workspaceSlug && !!reportId,
  });
}

export function publicDevReportRevisionsOptions(workspaceSlug: string, reportId: string) {
  return queryOptions({
    queryKey: publicDevReportKeys.revisions(workspaceSlug, reportId),
    queryFn: () => api.listPublicDevReportRevisions(workspaceSlug, reportId),
    select: (data) => data.revisions,
    enabled: !!workspaceSlug && !!reportId,
  });
}

