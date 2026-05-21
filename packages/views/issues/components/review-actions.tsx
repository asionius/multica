import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { CheckCircle2, MessageSquareWarning } from "lucide-react";
import { useT } from "../../i18n";
import type { Issue } from "@multica/core/types/issue";
import type { User } from "@multica/core/types/workspace";

interface ReviewActionsProps {
  issue: Pick<Issue, "status" | "assignee_id">;
  user: Pick<User, "id" | "name"> | null;
  onSubmit: (content: string) => Promise<void>;
}

/**
 * Approve / Request-Changes UI shown only when:
 *   - issue is in_review
 *   - there is a logged-in user
 *   - the user is not the assignee (prevents self-review)
 *
 * Approve → posts a comment with the magic header:
 *   ### [REVIEW] approve
 *   Approved by @<name> at <ISO date>
 *
 * Request Changes → opens inline textarea, Submit posts:
 *   ### [REVIEW] deny
 *   <user feedback>
 *
 * The agent that owns the issue watches for these magic headers and drives
 * the state machine transitions.
 */
export function ReviewActions({ issue, user, onSubmit }: ReviewActionsProps) {
  const { t } = useT("issues");
  const [mode, setMode] = useState<"idle" | "deny">("idle");
  const [feedback, setFeedback] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (issue.status !== "in_review") return null;
  if (!user) return null;
  if (issue.assignee_id && issue.assignee_id === user.id) return null;

  const today = new Date().toISOString().slice(0, 10);
  const displayName = user.name || "reviewer";

  const handleApprove = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(
        `### [REVIEW] approve\nApproved by @${displayName} at ${today}`,
      );
      toast.success(t(($) => $.review.approvedToast));
    } catch {
      toast.error(t(($) => $.review.approveFailedToast));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDenySubmit = async () => {
    const trimmed = feedback.trim();
    if (!trimmed || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(`### [REVIEW] deny\n${trimmed}`);
      toast.success(t(($) => $.review.requestSentToast));
      setFeedback("");
      setMode("idle");
    } catch {
      toast.error(t(($) => $.review.requestFailedToast));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mb-3 rounded-lg border bg-muted/40 p-3">
      <div className="mb-2 flex items-center justify-between">
        <div className="text-sm font-medium">{t(($) => $.review.awaitingTitle)}</div>
        {mode === "deny" && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setMode("idle");
              setFeedback("");
            }}
            disabled={submitting}
          >
            {t(($) => $.review.cancel)}
          </Button>
        )}
      </div>

      {mode === "idle" ? (
        <div className="flex gap-2">
          <Button
            variant="default"
            size="sm"
            onClick={handleApprove}
            disabled={submitting}
          >
            <CheckCircle2 className="mr-1 size-4" />
            {t(($) => $.review.approve)}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setMode("deny")}
            disabled={submitting}
          >
            <MessageSquareWarning className="mr-1 size-4" />
            {t(($) => $.review.requestChanges)}
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          <Textarea
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder={t(($) => $.review.feedbackPlaceholder)}
            rows={3}
            disabled={submitting}
            autoFocus
          />
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={handleDenySubmit}
              disabled={submitting || feedback.trim().length === 0}
            >
              {t(($) => $.review.sendRequest)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
