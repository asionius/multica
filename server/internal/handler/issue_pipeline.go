package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssuePipelineResponse mirrors the three structured columns added by
// migration 097. Legacy issues (pipeline_kind=NULL) return Kind="" and the
// JSON values as null/empty — callers fall back to parsing description YAML
// during the dual-read compatibility window.
type IssuePipelineResponse struct {
	Kind    string          `json:"kind"`
	Base    json.RawMessage `json:"base"`
	Dynamic json.RawMessage `json:"dynamic"`
}

// GetIssuePipeline returns the structured pipeline state of an issue.
// GET /api/issues/{id}/pipeline
//
// pipeline_base / pipeline_dynamic are exposed as opaque JSON to the caller.
// The shape is owned by the pipeline-initializer agent; the server treats
// these as data blobs and only enforces "must be valid JSON" on writes.
func (h *Handler) GetIssuePipeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	row, err := h.Queries.GetIssuePipelineState(r.Context(), issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load pipeline state")
		return
	}
	writeJSON(w, http.StatusOK, issuePipelineRowToResponse(row))
}

// PatchIssuePipelineBase writes the static topology half of pipeline_state.
// PATCH /api/issues/{id}/pipeline/base
//
// Body: { "kind": "ccproc-stage", "base": { ... } }
//
// kind is a free-form string (currently expected: ccproc-top / ccproc-stage /
// ccproc-packager) — server does not enumerate-validate, that's a contract
// between the initializer agent and the stage agents that read it.
//
// By convention only the pipeline-initializer agent calls this. Stage agents
// must use the verdict path (planned in a follow-up PR) for atomic dynamic
// updates.
func (h *Handler) PatchIssuePipelineBase(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	var body struct {
		Kind string          `json:"kind"`
		Base json.RawMessage `json:"base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}
	if len(body.Base) == 0 || !json.Valid(body.Base) {
		writeError(w, http.StatusBadRequest, "base must be a JSON object")
		return
	}
	if err := h.Queries.SetIssuePipelineBase(r.Context(), db.SetIssuePipelineBaseParams{
		ID:           issue.ID,
		PipelineKind: pgtype.Text{String: body.Kind, Valid: true},
		PipelineBase: []byte(body.Base),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pipeline base")
		return
	}
	row, err := h.Queries.GetIssuePipelineState(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read pipeline state")
		return
	}
	writeJSON(w, http.StatusOK, issuePipelineRowToResponse(row))
}

// PatchIssuePipelineDynamic writes the mutable counter half.
// PATCH /api/issues/{id}/pipeline/dynamic
//
// Body: { "dynamic": { ... } }
//
// Kept separate from base so prefix-cache-friendly topology stays untouched
// on every ping-pong.
func (h *Handler) PatchIssuePipelineDynamic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	var body struct {
		Dynamic json.RawMessage `json:"dynamic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Dynamic) == 0 || !json.Valid(body.Dynamic) {
		writeError(w, http.StatusBadRequest, "dynamic must be valid JSON")
		return
	}
	if err := h.Queries.SetIssuePipelineDynamic(r.Context(), db.SetIssuePipelineDynamicParams{
		ID:              issue.ID,
		PipelineDynamic: []byte(body.Dynamic),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pipeline dynamic")
		return
	}
	row, err := h.Queries.GetIssuePipelineState(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read pipeline state")
		return
	}
	writeJSON(w, http.StatusOK, issuePipelineRowToResponse(row))
}

func issuePipelineRowToResponse(row db.GetIssuePipelineStateRow) IssuePipelineResponse {
	resp := IssuePipelineResponse{}
	if row.PipelineKind.Valid {
		resp.Kind = row.PipelineKind.String
	}
	if len(row.PipelineBase) > 0 {
		resp.Base = json.RawMessage(row.PipelineBase)
	}
	if len(row.PipelineDynamic) > 0 {
		resp.Dynamic = json.RawMessage(row.PipelineDynamic)
	}
	return resp
}
