package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ConsumeIssueEvent handles POST /api/daemon/issue-events/{eventId}/consume.
// Called by the daemon right before executing an agent task to mark the
// IssueEvent as consumed, preventing double-delivery on task retry.
func (h *Handler) ConsumeIssueEvent(w http.ResponseWriter, r *http.Request) {
	eventIDStr := chi.URLParam(r, "eventId")

	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	eventUUID, err := util.ParseUUID(eventIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event_id")
		return
	}

	taskUUID, err := util.ParseUUID(req.TaskID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	if err := h.Queries.MarkIssueEventConsumed(r.Context(), db.MarkIssueEventConsumedParams{
		ID:               eventUUID,
		ConsumedByTaskID: taskUUID,
	}); err != nil {
		slog.Error("mark issue event consumed failed", "event_id", eventIDStr, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to mark event consumed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
