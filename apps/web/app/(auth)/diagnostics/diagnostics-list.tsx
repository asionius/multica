"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { api } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import type { SessionDiagnostic } from "@multica/core/types";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { Download, ExternalLink } from "lucide-react";

export function DiagnosticsList() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["diagnostics"],
    queryFn: () => api.listDiagnostics(),
  });

  const items = data?.diagnostics ?? [];

  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <div>
        <h1 className="text-2xl font-semibold">Session diagnostics</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Packed CLI/plugin sessions. Admins see every upload; everyone else
          sees their own.
        </p>
      </div>

      {isLoading ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : error ? (
        <div className="space-y-3">
          <p className="text-sm text-destructive">
            {error instanceof Error ? error.message : "Failed to load diagnostics"}
          </p>
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            Retry
          </Button>
        </div>
      ) : items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No diagnostics yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Session</TableHead>
              <TableHead>Uploader</TableHead>
              <TableHead>Files</TableHead>
              <TableHead>Size</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((row) => (
              <DiagnosticRow key={row.id} row={row} />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function DiagnosticRow({ row }: { row: SessionDiagnostic }) {
  return (
    <TableRow>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {formatTime(row.created_at)}
      </TableCell>
      <TableCell className="max-w-[220px] truncate font-mono text-xs">
        {row.session_id || "—"}
      </TableCell>
      <TableCell className="max-w-[220px] truncate">
        {row.uploader_name || row.uploader_email || "—"}
      </TableCell>
      <TableCell>{row.file_count}</TableCell>
      <TableCell>{formatBytes(row.size_bytes)}</TableCell>
      <TableCell className="text-right">
        <div className="flex justify-end gap-2">
          <Link
            href={paths.diagnosticDetail(row.id)}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            <ExternalLink className="size-3.5" />
            View
          </Link>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void downloadDiagnosticArchive(row)}
          >
            <Download className="size-3.5" />
            Download
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

export async function downloadDiagnosticArchive(row: SessionDiagnostic) {
  const blob = await api.downloadDiagnostic(row.id);
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = row.filename || `diagnostic-${row.id}.tar.gz`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export function formatTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
