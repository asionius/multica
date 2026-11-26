import { useParams } from "react-router-dom";
import { DevReportPage } from "@multica/views/dev-reports";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function DevReportDetailPage() {
  const { reportId } = useParams<{ reportId: string }>();
  useDocumentTitle("Dev report");
  if (!reportId) return null;
  return <DevReportPage reportId={reportId} />;
}
