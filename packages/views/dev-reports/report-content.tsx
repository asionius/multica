"use client";

import { useState } from "react";
import { Check, Copy, Download, Printer } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import type { DevReportDetail } from "@multica/core/types";
import { Markdown } from "../common/markdown";
import { useT } from "../i18n";

function formatInstant(value: string): string {
  if (!value) return "—";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function formatFileStamp(report: DevReportDetail): string {
  const version = report.version.replace(/[^a-zA-Z0-9._-]+/g, "-");
  return `dev-report-${version}-r${report.revision}.md`;
}

/**
 * Renders one immutable report revision: a compact metadata line, the
 * copy/download/print actions, and the approved Markdown body verbatim.
 *
 * The body is the only content source. Nothing here recomputes percentages,
 * fills gaps as zero, or rewrites wording — the Markdown component keeps the
 * existing sanitize chain.
 */
export function DevReportContent({
  report,
  fetchMarkdown,
}: {
  report: DevReportDetail;
  /** Public pages pass the unauthenticated downloader; members use the API default. */
  fetchMarkdown?: (id: string) => Promise<string>;
}) {
  const { t } = useT("dev-reports");
  const [copied, setCopied] = useState(false);
  const [actionFailed, setActionFailed] = useState(false);

  const handleCopy = async () => {
    setActionFailed(false);
    try {
      await navigator.clipboard.writeText(report.body_markdown);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setActionFailed(true);
    }
  };

  const handleDownload = async () => {
    setActionFailed(false);
    try {
      // Fetched with auth headers so the download can never be built from a
      // client-supplied path.
      const markdown = await (fetchMarkdown ?? api.getDevReportMarkdown)(report.id);
      const blob = new Blob([markdown], { type: "text/markdown;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = formatFileStamp(report);
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch {
      setActionFailed(true);
    }
  };

  return (
    <article className="dev-report-print-root mx-auto w-full max-w-[1320px] px-4 py-6 md:px-8">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-3 border-b pb-4">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
          <span className="font-medium text-foreground">
            {t(($) => $.version_label)} {report.version}
          </span>
          {report.comparison_version ? (
            <span>
              {t(($) => $.comparison_label, { version: report.comparison_version })}
            </span>
          ) : null}
          <span>
            {t(($) => $.observation_label)} {formatInstant(report.observation_as_of)}
          </span>
          <span>
            {t(($) => $.published_label)} {formatInstant(report.published_at)}
          </span>
          <span className="rounded border px-1.5 py-0.5 text-xs">
            {t(($) => $.revision_label, { revision: report.revision })}
          </span>
        </div>

        <div className="flex items-center gap-2 print:hidden">
          <Button size="sm" variant="outline" onClick={handleCopy}>
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {copied ? t(($) => $.actions.copied) : t(($) => $.actions.copy)}
          </Button>
          <Button size="sm" variant="outline" onClick={handleDownload}>
            <Download className="size-3.5" />
            {t(($) => $.actions.download)}
          </Button>
          <Button size="sm" variant="outline" onClick={() => window.print()}>
            <Printer className="size-3.5" />
            {t(($) => $.actions.print)}
          </Button>
        </div>
      </div>

      {actionFailed ? (
        <p className="mb-4 text-sm text-destructive" role="alert">
          {t(($) => $.error.action_failed)}
        </p>
      ) : null}

      {/*
        Table scroll containment is scoped to this wrapper so a wide quality
        table scrolls locally instead of stretching the page, and so issue
        Markdown elsewhere keeps its normal layout.
      */}
      <div className="dev-report-body text-sm leading-6 [&_table]:my-3 [&_table]:block [&_table]:w-full [&_table]:overflow-x-auto [&_table]:whitespace-nowrap [&_td]:align-top [&_th]:whitespace-normal">
        <Markdown mode="full">{report.body_markdown}</Markdown>
      </div>
    </article>
  );
}
