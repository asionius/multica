export interface SessionDiagnosticFileEntry {
  path: string;
  size: number;
}

export interface SessionDiagnostic {
  id: string;
  session_id: string;
  hostname: string;
  plugin_version: string;
  filename: string;
  size_bytes: number;
  file_count: number;
  file_manifest: SessionDiagnosticFileEntry[];
  metadata: Record<string, unknown>;
  uploader_name: string;
  uploader_email: string;
  created_at: string;
  view_url: string;
  download_url: string;
}

export interface ListSessionDiagnosticsResponse {
  diagnostics: SessionDiagnostic[];
}
