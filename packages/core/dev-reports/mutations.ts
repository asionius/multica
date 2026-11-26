import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { devReportKeys } from "./queries";
import type { PublishDevReportRequest } from "../types";

/**
 * Publishing is the only mutation this surface exposes. It always creates a
 * new immutable revision server-side; the client never edits or deletes.
 */
export function usePublishDevReport() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: PublishDevReportRequest) => api.publishDevReport(data),
    onSuccess: (report) => {
      qc.invalidateQueries({ queryKey: devReportKeys.lists(wsId) });
      qc.invalidateQueries({ queryKey: devReportKeys.detail(wsId, report.id) });
    },
  });
}
