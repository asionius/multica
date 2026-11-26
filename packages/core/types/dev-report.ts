// A published, immutable revision of a development operations report.
//
// The body is stored server-side as approved Markdown and is the only content
// source the UI renders — no metric recomputation happens on the client.
// Metadata exists to scope the report to a workspace/version and to support
// revision history, not to duplicate the numbers inside the body.
export interface DevReport {
  id: string;
  workspace_id: string;
  project_id: string | null;
  /** Version key, e.g. "3.34.0". Compared as an exact key, never ordered lexically. */
  version: string;
  comparison_version: string | null;
  title: string;
  /** Publishing document schema version (not the quality-result schema). */
  schema_version: number;
  /** Metric/historical-inference version declared by the report author. */
  metric_version: string;
  /** ISO-8601 instant with timezone — when the observation window ended. */
  observation_as_of: string;
  /** Monotonic per workspace+version, starting at 1. */
  revision: number;
  body_sha256: string;
  published_by: string;
  published_at: string;
}

/** Detail payload adds the stored Markdown body verbatim. */
export interface DevReportDetail extends DevReport {
  body_markdown: string;
}

export interface ListDevReportsResponse {
  reports: DevReport[];
  total: number;
}

export interface ListDevReportRevisionsResponse {
  revisions: DevReport[];
  total: number;
}

export interface ListDevReportsParams {
  version?: string;
  limit?: number;
  offset?: number;
}

export interface PublishDevReportRequest {
  schema_version: number;
  version: string;
  comparison_version?: string | null;
  project_id?: string | null;
  title: string;
  metric_version: string;
  /** RFC3339 timestamp that includes an explicit timezone. */
  observation_as_of: string;
  body_markdown: string;
  /** Idempotency key; reusing it with different content is rejected. */
  publish_key: string;
}

/** Publish returns the metadata of the revision it created (or replayed). */
export type PublishDevReportResponse = DevReport;

export interface DevReportPublishedPayload {
  workspace_id: string;
  report_id: string;
  version: string;
  revision: number;
}
