"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { api, PreviewTooLargeError } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import type { SessionDiagnosticFileEntry } from "@multica/core/types";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { ArrowLeft, Download, FileJson } from "lucide-react";
import {
  downloadDiagnosticArchive,
  formatBytes,
  formatTime,
} from "../diagnostics-list";

export function DiagnosticDetail({ id }: { id: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["diagnostics", id],
    queryFn: () => api.getDiagnostic(id),
  });
  const [selectedPath, setSelectedPath] = useState<string | null>(null);

  const files = data?.file_manifest ?? [];

  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-1">
          <Link
            href={paths.diagnostics()}
            className={`${buttonVariants({ variant: "ghost", size: "sm" })} -ml-2`}
          >
            <ArrowLeft className="size-3.5" />
            All diagnostics
          </Link>
          <h1 className="text-2xl font-semibold">Diagnostic</h1>
        </div>
        {data && data.id !== "" && (
          <Button onClick={() => void downloadDiagnosticArchive(data)}>
            <Download className="size-3.5" />
            Download archive
          </Button>
        )}
      </div>

      {isLoading ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : error || !data || data.id === "" ? (
        <p className="text-sm text-destructive">
          {error instanceof Error ? error.message : "Diagnostic not found"}
        </p>
      ) : (
        <>
          <dl className="grid grid-cols-1 gap-3 rounded-lg border border-border p-4 text-sm sm:grid-cols-2">
            <Meta label="Uploaded" value={formatTime(data.created_at)} />
            <Meta label="Session" value={data.session_id || "—"} mono />
            <Meta
              label="Uploader"
              value={
                [data.uploader_name, data.uploader_email].filter(Boolean).join(" · ") ||
                "—"
              }
            />
            <Meta label="Hostname" value={data.hostname || "—"} />
            <Meta label="Plugin" value={data.plugin_version || "—"} />
            <Meta label="Size" value={formatBytes(data.size_bytes)} />
            <Meta label="Files" value={String(data.file_count)} />
            <Meta label="Filename" value={data.filename || "—"} mono />
            {noteFromMetadata(data.metadata) && (
              <Meta label="Note" value={noteFromMetadata(data.metadata) ?? ""} />
            )}
          </dl>

          <div className="grid min-h-[28rem] grid-cols-1 gap-4 lg:grid-cols-[minmax(16rem,22rem)_1fr]">
            <div className="rounded-lg border border-border">
              <div className="border-b border-border px-3 py-2 text-sm font-medium">
                Files
              </div>
              <ul className="max-h-[32rem] overflow-auto p-1">
                {files.length === 0 ? (
                  <li className="px-2 py-3 text-sm text-muted-foreground">
                    Empty manifest
                  </li>
                ) : (
                  files.map((file) => (
                    <FileRow
                      key={file.path}
                      file={file}
                      selected={selectedPath === file.path}
                      onSelect={() => setSelectedPath(file.path)}
                    />
                  ))
                )}
              </ul>
            </div>
            <FilePreview diagnosticId={id} path={selectedPath} />
          </div>
        </>
      )}
    </div>
  );
}

function Meta({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? "truncate font-mono text-xs" : "truncate"}>{value}</dd>
    </div>
  );
}

function FileRow({
  file,
  selected,
  onSelect,
}: {
  file: SessionDiagnosticFileEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        className={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-sm ${
          selected ? "bg-accent" : "hover:bg-accent/50"
        }`}
      >
        <span className="flex min-w-0 items-center gap-2">
          <FileJson className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate font-mono text-xs">{file.path}</span>
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">
          {formatBytes(file.size)}
        </span>
      </button>
    </li>
  );
}

function FilePreview({
  diagnosticId,
  path,
}: {
  diagnosticId: string;
  path: string | null;
}) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["diagnostics", diagnosticId, "file", path],
    queryFn: () => api.getDiagnosticFile(diagnosticId, path ?? ""),
    enabled: path !== null && path !== "",
  });

  const pretty = useMemo(() => prettyPrint(data), [data]);

  return (
    <div className="flex min-h-[20rem] flex-col rounded-lg border border-border">
      <div className="border-b border-border px-3 py-2 text-sm font-medium">
        {path ?? "Select a file"}
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        {!path ? (
          <p className="text-sm text-muted-foreground">
            Choose a file from the tree to preview it (up to 2 MB).
          </p>
        ) : isLoading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : error instanceof PreviewTooLargeError ? (
          <p className="text-sm text-muted-foreground">
            File is larger than 2 MB. Download the archive to inspect it.
          </p>
        ) : error ? (
          <p className="text-sm text-destructive">
            {error instanceof Error ? error.message : "Failed to load file"}
          </p>
        ) : (
          <pre className="whitespace-pre-wrap break-all font-mono text-xs">
            {pretty}
          </pre>
        )}
      </div>
    </div>
  );
}

function prettyPrint(text: string | undefined): string {
  if (text === undefined) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function noteFromMetadata(metadata: Record<string, unknown> | undefined): string | null {
  const note = metadata?.note;
  return typeof note === "string" && note !== "" ? note : null;
}
