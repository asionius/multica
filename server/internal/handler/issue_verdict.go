package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// verdictRequest is the body of POST /api/issues/{id}/verdict.
type verdictRequest struct {
	Action   string `json:"action"`   // "approve", "deny", or "escalate"
	Feedback string `json:"feedback"` // optional human-readable note
}

// verdictResponse is returned after a successful verdict execution.
type verdictResponse struct {
	OK              bool                   `json:"ok"`
	Action          string                 `json:"action"`
	IssueID         string                 `json:"issue_id"`
	NewStatus       string                 `json:"new_status"`
	PipelineDynamic map[string]interface{} `json:"pipeline_dynamic,omitempty"`
	AffectedIssues  []string               `json:"affected_issues,omitempty"`
}

// PostIssueVerdict handles POST /api/issues/{id}/verdict.
// The agent calls this as the single atomic write-point for pipeline
// state transitions, replacing the old read-modify-write cycle of
// `issue status` + `pipeline set --dynamic` calls that had no atomicity.
func (h *Handler) PostIssueVerdict(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	var req verdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action != "approve" && req.Action != "deny" && req.Action != "escalate" && req.Action != "bounce" {
		writeError(w, http.StatusBadRequest, "action must be approve, deny, bounce, or escalate")
		return
	}

	// Load issue via the standard workspace-auth path.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	// Only pipeline issues support verdict.
	pipeline, err := h.Queries.GetIssuePipelineState(r.Context(), issue.ID)
	if err != nil || !pipeline.PipelineKind.Valid || pipeline.PipelineKind.String == "" {
		writeError(w, http.StatusBadRequest, "issue has no pipeline state")
		return
	}

	// Parse pipeline_base to find prev/next and bounce_cap.
	var base map[string]interface{}
	if len(pipeline.PipelineBase) > 0 {
		if err := json.Unmarshal(pipeline.PipelineBase, &base); err != nil {
			writeError(w, http.StatusInternalServerError, "invalid pipeline_base JSON")
			return
		}
	}

	// Resolve prev/next from siblings.
	prevIDStr, nextIDStr := resolveVerdictNeighbors(base, uuidToString(issue.ID))

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("verdict begin tx failed", "error", err)
		writeError(w, http.StatusInternalServerError, "transaction begin failed")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	resp := verdictResponse{OK: true, Action: req.Action, IssueID: issueID}

	switch req.Action {
	case "approve":
		if issue.Status != "in_review" {
			writeError(w, http.StatusConflict, fmt.Sprintf("cannot approve: issue status must be in_review, got %s", issue.Status))
			return
		}
		// self → done
		if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
			ID: issue.ID, Status: "done",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set self status")
			return
		}
		resp.NewStatus = "done"

		// next: status → todo, bounce_with_prev += 1
		if nextIDStr != "" {
			nextUUID, err := util.ParseUUID(nextIDStr)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "invalid next_issue_id in pipeline_base")
				return
			}
			if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
				ID: nextUUID, Status: "todo",
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to set next status")
				return
			}
			if err := qtx.IncrementBounceWithPrev(r.Context(), nextUUID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to increment bounce_with_prev")
				return
			}
			resp.AffectedIssues = append(resp.AffectedIssues, nextIDStr)
		}

	case "deny":
		if issue.Status != "in_review" {
			writeError(w, http.StatusConflict, fmt.Sprintf("cannot deny: issue status must be in_review, got %s", issue.Status))
			return
		}
		// self → in_progress
		if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
			ID: issue.ID, Status: "in_progress",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set self status")
			return
		}
		resp.NewStatus = "in_progress"

		// prev: status → in_progress, bounce_with_next += 1, cap check → escalated
		if prevIDStr != "" {
			prevUUID, err := util.ParseUUID(prevIDStr)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "invalid prev_issue_id in pipeline_base")
				return
			}
			if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
				ID: prevUUID, Status: "in_progress",
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to set prev status")
				return
			}
			result, err := qtx.IncrementBounceWithNext(r.Context(), prevUUID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to increment bounce_with_next")
				return
			}
			// Cap check: if bounce_with_next >= bounce_cap, set escalated=true.
			if result.NewBounceWithNext >= int32(result.BounceCap) && result.BounceCap > 0 {
				if err := qtx.SetIssuePipelineEscalated(r.Context(), prevUUID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to set escalated")
					return
				}
			}
			resp.AffectedIssues = append(resp.AffectedIssues, prevIDStr)
		}

	case "bounce":
		// Agent-initiated rollback: self → blocked, prev → todo,
		// prev.bounce_with_next += 1 with cap check.
		if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
			ID: issue.ID, Status: "blocked",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set self status")
			return
		}
		resp.NewStatus = "blocked"

		if prevIDStr != "" {
			prevUUID, err := util.ParseUUID(prevIDStr)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "invalid prev_issue_id in pipeline_base")
				return
			}
			if _, err := qtx.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
				ID: prevUUID, Status: "todo",
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to set prev status")
				return
			}
			result, err := qtx.IncrementBounceWithNext(r.Context(), prevUUID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to increment bounce_with_next")
				return
			}
			if result.NewBounceWithNext >= int32(result.BounceCap) && result.BounceCap > 0 {
				if err := qtx.SetIssuePipelineEscalated(r.Context(), prevUUID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to set escalated")
					return
				}
			}
			resp.AffectedIssues = append(resp.AffectedIssues, prevIDStr)
		}

	case "escalate":
		if err := qtx.SetIssuePipelineEscalated(r.Context(), issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set escalated")
			return
		}
		resp.NewStatus = issue.Status // status unchanged by escalate
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("verdict commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "transaction commit failed")
		return
	}

	// Read the updated pipeline_dynamic for the response.
	after, err := h.Queries.GetIssuePipelineState(r.Context(), issue.ID)
	if err == nil && len(after.PipelineDynamic) > 0 {
		var dyn map[string]interface{}
		if json.Unmarshal(after.PipelineDynamic, &dyn) == nil {
			resp.PipelineDynamic = dyn
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// resolveVerdictNeighbors walks pipeline_base.siblings to find the prev
// and next issue IDs relative to the given self ID string.
func resolveVerdictNeighbors(base map[string]interface{}, selfID string) (prevID, nextID string) {
	if base == nil {
		return "", ""
	}
	raw, ok := base["siblings"]
	if !ok {
		return "", ""
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return "", ""
	}
	idx := -1
	for i, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := obj["id"].(string); id == selfID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", ""
	}
	if idx > 0 {
		if obj, ok := arr[idx-1].(map[string]interface{}); ok {
			prevID, _ = obj["id"].(string)
		}
	}
	if idx < len(arr)-1 {
		if obj, ok := arr[idx+1].(map[string]interface{}); ok {
			nextID, _ = obj["id"].(string)
		}
	}
	return prevID, nextID
}
