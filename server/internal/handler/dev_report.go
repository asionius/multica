package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// devReportSchemaVersion is the only payload schema this server accepts. It
// describes the publish request/response document shape — not the quality
// result schema and not the metric semantics inside body_markdown.
const devReportSchemaVersion = 1

// maxDevReportRequestBytes caps the publish payload (1 MiB). The approved CC
// report is ~8 KB; the headroom is for future versions with more embedded
// evidence, not for attachments.
const maxDevReportRequestBytes = 1 << 20

const (
	devReportDefaultListLimit = 20
	devReportMaxListLimit     = 100
	devReportMaxVersionLen    = 64
	devReportMaxTitleLen      = 200
	devReportMaxMetricLen     = 200
	devReportMaxPublishKeyLen = 128
)

// DevReportResponse is the metadata shape shared by list and detail. Body is
// only populated on detail/download responses so listing a workspace stays
// cheap.
type DevReportResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	ProjectID         *string `json:"project_id"`
	Version           string  `json:"version"`
	ComparisonVersion *string `json:"comparison_version"`
	Title             string  `json:"title"`
	SchemaVersion     int32   `json:"schema_version"`
	MetricVersion     string  `json:"metric_version"`
	ObservationAsOf   string  `json:"observation_as_of"`
	Revision          int32   `json:"revision"`
	BodySHA256        string  `json:"body_sha256"`
	PublishedBy       string  `json:"published_by"`
	PublishedAt       string  `json:"published_at"`
	BodyMarkdown      *string `json:"body_markdown,omitempty"`
}

type ListDevReportsResponse struct {
	Reports []DevReportResponse `json:"reports"`
	Total   int64               `json:"total"`
}

type ListDevReportRevisionsResponse struct {
	Revisions []DevReportResponse `json:"revisions"`
	Total     int64               `json:"total"`
}

func devReportMetadataFromFields(
	id pgtype.UUID,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	version string,
	comparisonVersion pgtype.Text,
	title string,
	schemaVersion int32,
	metricVersion string,
	observationAsOf pgtype.Timestamptz,
	revision int32,
	bodySHA256 string,
	publishedBy pgtype.UUID,
	publishedAt pgtype.Timestamptz,
) DevReportResponse {
	return DevReportResponse{
		ID:                uuidToString(id),
		WorkspaceID:       uuidToString(workspaceID),
		ProjectID:         uuidToPtr(projectID),
		Version:           version,
		ComparisonVersion: textToPtr(comparisonVersion),
		Title:             title,
		SchemaVersion:     schemaVersion,
		MetricVersion:     metricVersion,
		ObservationAsOf:   devReportTimeToString(observationAsOf),
		Revision:          revision,
		BodySHA256:        bodySHA256,
		PublishedBy:       uuidToString(publishedBy),
		PublishedAt:       devReportTimeToString(publishedAt),
	}
}

// devReportTimeToString renders a timestamptz in UTC so the public API shape
// does not depend on the server's session timezone. The instant is unchanged.
func devReportTimeToString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func devReportToResponse(r db.DevReport, includeBody bool) DevReportResponse {
	resp := devReportMetadataFromFields(
		r.ID, r.WorkspaceID, r.ProjectID, r.Version, r.ComparisonVersion, r.Title,
		r.SchemaVersion, r.MetricVersion, r.ObservationAsOf, r.Revision,
		r.BodySha256, r.PublishedBy, r.PublishedAt,
	)
	if includeBody {
		body := r.BodyMarkdown
		resp.BodyMarkdown = &body
	}
	return resp
}

func devReportRowToResponse(r db.ListLatestDevReportsRow) DevReportResponse {
	return devReportMetadataFromFields(
		r.ID, r.WorkspaceID, r.ProjectID, r.Version, r.ComparisonVersion, r.Title,
		r.SchemaVersion, r.MetricVersion, r.ObservationAsOf, r.Revision,
		r.BodySha256, r.PublishedBy, r.PublishedAt,
	)
}

// ListDevReports returns the highest revision of every published version in
// the current workspace, newest observation first. Bodies are omitted.
func (h *Handler) ListDevReports(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}

	limit, offset := parseBoundedPagination(r, devReportDefaultListLimit, devReportMaxListLimit)

	var versionFilter pgtype.Text
	if v := strings.TrimSpace(r.URL.Query().Get("version")); v != "" {
		if len(v) > devReportMaxVersionLen {
			writeError(w, http.StatusBadRequest, "version filter is too long")
			return
		}
		versionFilter = pgtype.Text{String: v, Valid: true}
	}

	rows, err := h.Queries.ListLatestDevReports(r.Context(), db.ListLatestDevReportsParams{
		WorkspaceID: wsUUID,
		Column2:     limit,
		Column3:     offset,
		Version:     versionFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dev reports")
		return
	}
	total, err := h.Queries.CountLatestDevReports(r.Context(), db.CountLatestDevReportsParams{
		WorkspaceID: wsUUID,
		Version:     versionFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count dev reports")
		return
	}

	reports := make([]DevReportResponse, len(rows))
	for i, row := range rows {
		reports[i] = devReportRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, ListDevReportsResponse{Reports: reports, Total: total})
}

// ListDevReportRevisions returns every revision of the version that the
// referenced report belongs to, newest first. Metadata only.
func (h *Handler) ListDevReportRevisions(w http.ResponseWriter, r *http.Request) {
	report, ok := h.lookupDevReportInWorkspace(w, r)
	if !ok {
		return
	}
	limit, offset := parseBoundedPagination(r, devReportMaxListLimit, devReportMaxListLimit)

	rows, err := h.Queries.ListDevReportRevisions(r.Context(), db.ListDevReportRevisionsParams{
		WorkspaceID: report.WorkspaceID,
		Version:     report.Version,
		Column3:     limit,
		Column4:     offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dev report revisions")
		return
	}
	total, err := h.Queries.CountDevReportRevisions(r.Context(), db.CountDevReportRevisionsParams{
		WorkspaceID: report.WorkspaceID,
		Version:     report.Version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count dev report revisions")
		return
	}

	revisions := make([]DevReportResponse, len(rows))
	for i, row := range rows {
		revisions[i] = devReportMetadataFromFields(
			row.ID, row.WorkspaceID, row.ProjectID, row.Version, row.ComparisonVersion, row.Title,
			row.SchemaVersion, row.MetricVersion, row.ObservationAsOf, row.Revision,
			row.BodySha256, row.PublishedBy, row.PublishedAt,
		)
	}
	writeJSON(w, http.StatusOK, ListDevReportRevisionsResponse{Revisions: revisions, Total: total})
}

// GetDevReport returns one immutable revision scoped to the current workspace.
func (h *Handler) GetDevReport(w http.ResponseWriter, r *http.Request) {
	report, ok := h.lookupDevReportInWorkspace(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, devReportToResponse(report, true))
}

// DownloadDevReportMarkdown streams the stored body verbatim. The body always
// comes from the DB; the request never names a path or URL.
func (h *Handler) DownloadDevReportMarkdown(w http.ResponseWriter, r *http.Request) {
	report, ok := h.lookupDevReportInWorkspace(w, r)
	if !ok {
		return
	}
	filename := fmt.Sprintf("dev-report-%s-r%d.md", sanitizeFilenamePart(report.Version), report.Revision)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(report.BodyMarkdown))
}

func (h *Handler) lookupDevReportInWorkspace(w http.ResponseWriter, r *http.Request) (db.DevReport, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "report id")
	if !ok {
		return db.DevReport{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.DevReport{}, false
	}
	report, err := h.Queries.GetDevReportInWorkspace(r.Context(), db.GetDevReportInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		// Missing and cross-workspace are deliberately indistinguishable.
		writeError(w, http.StatusNotFound, "dev report not found")
		return db.DevReport{}, false
	}
	return report, true
}

type PublishDevReportRequest struct {
	SchemaVersion     int32   `json:"schema_version"`
	Version           string  `json:"version"`
	ComparisonVersion *string `json:"comparison_version"`
	ProjectID         *string `json:"project_id"`
	Title             string  `json:"title"`
	MetricVersion     string  `json:"metric_version"`
	ObservationAsOf   string  `json:"observation_as_of"`
	BodyMarkdown      string  `json:"body_markdown"`
	PublishKey        string  `json:"publish_key"`
}

// devReportDigestPayload is the canonical set of content that identifies a
// publish request. It is serialized with a fixed field order so identical
// content produces an identical digest regardless of JSON key order or
// timestamp offset in the client request.
type devReportDigestPayload struct {
	SchemaVersion     int32   `json:"schema_version"`
	Version           string  `json:"version"`
	ComparisonVersion *string `json:"comparison_version"`
	ProjectID         *string `json:"project_id"`
	Title             string  `json:"title"`
	MetricVersion     string  `json:"metric_version"`
	ObservationAsOf   string  `json:"observation_as_of"`
	BodySHA256        string  `json:"body_sha256"`
}

type normalizedDevReportRequest struct {
	version           string
	comparisonVersion pgtype.Text
	projectID         pgtype.UUID
	projectIDString   *string
	title             string
	metricVersion     string
	observationAsOf   pgtype.Timestamptz
	bodyMarkdown      string
	bodySHA256        string
	publishKey        string
	requestDigest     string
}

// PublishDevReport appends a new immutable revision. The workspace always
// comes from the authenticated request context; the body can never choose it.
func (h *Handler) PublishDevReport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDevReportRequestBytes)

	var req PublishDevReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized, ok := h.normalizeDevReportRequest(w, r, &req)
	if !ok {
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	publishedBy, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	// Fast path: an existing publish key with the same content is a replay.
	if existing, err := h.Queries.GetDevReportByPublishKey(r.Context(), db.GetDevReportByPublishKeyParams{
		WorkspaceID: wsUUID,
		PublishKey:  normalized.publishKey,
	}); err == nil {
		h.respondToPublishReplay(w, existing, normalized.requestDigest)
		return
	} else if !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to check publish key")
		return
	}

	report, ok := h.insertDevReportRevision(w, r, wsUUID, publishedBy, normalized)
	if !ok {
		return
	}

	h.publish(protocol.EventDevReportPublished, uuidToString(wsUUID), "member", uuidToString(publishedBy), map[string]any{
		"workspace_id": uuidToString(wsUUID),
		"report_id":    uuidToString(report.ID),
		"version":      report.Version,
		"revision":     report.Revision,
	})

	writeJSON(w, http.StatusCreated, devReportToResponse(report, false))
}

func (h *Handler) respondToPublishReplay(w http.ResponseWriter, existing db.DevReport, requestDigest string) {
	if existing.RequestDigest != requestDigest {
		writeError(w, http.StatusConflict, "publish_key already used with different content")
		return
	}
	writeJSON(w, http.StatusOK, devReportToResponse(existing, false))
}

func (h *Handler) insertDevReportRevision(
	w http.ResponseWriter,
	r *http.Request,
	wsUUID, publishedBy pgtype.UUID,
	normalized normalizedDevReportRequest,
) (db.DevReport, bool) {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return db.DevReport{}, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Serialize revision allocation per (workspace, version) so two concurrent
	// publishes can never both compute the same next revision.
	if err := qtx.LockDevReportVersion(r.Context(), db.LockDevReportVersionParams{
		Column1: uuidToString(wsUUID),
		Column2: normalized.version,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock report version")
		return db.DevReport{}, false
	}

	// Re-check the key under the lock: a concurrent request with the same key
	// may have committed between the fast path and here.
	existing, err := qtx.GetDevReportByPublishKey(r.Context(), db.GetDevReportByPublishKeyParams{
		WorkspaceID: wsUUID,
		PublishKey:  normalized.publishKey,
	})
	if err == nil {
		_ = tx.Rollback(r.Context())
		h.respondToPublishReplay(w, existing, normalized.requestDigest)
		return db.DevReport{}, false
	}
	if !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to check publish key")
		return db.DevReport{}, false
	}

	nextRevision := int32(1)
	latest, err := qtx.GetLatestDevReportRevision(r.Context(), db.GetLatestDevReportRevisionParams{
		WorkspaceID: wsUUID,
		Version:     normalized.version,
	})
	if err == nil {
		nextRevision = latest + 1
	} else if !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to allocate revision")
		return db.DevReport{}, false
	}

	report, err := qtx.CreateDevReport(r.Context(), db.CreateDevReportParams{
		WorkspaceID:       wsUUID,
		ProjectID:         normalized.projectID,
		Version:           normalized.version,
		ComparisonVersion: normalized.comparisonVersion,
		Title:             normalized.title,
		SchemaVersion:     devReportSchemaVersion,
		MetricVersion:     normalized.metricVersion,
		ObservationAsOf:   normalized.observationAsOf,
		Revision:          nextRevision,
		BodyMarkdown:      normalized.bodyMarkdown,
		BodySha256:        normalized.bodySHA256,
		PublishKey:        normalized.publishKey,
		RequestDigest:     normalized.requestDigest,
		PublishedBy:       publishedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			// A concurrent publisher (possibly on another version, hence a
			// different advisory lock) won the publish_key race. Release our
			// transaction and replay their result if it is the same content.
			_ = tx.Rollback(r.Context())
			if existing, lookupErr := h.Queries.GetDevReportByPublishKey(r.Context(), db.GetDevReportByPublishKeyParams{
				WorkspaceID: wsUUID,
				PublishKey:  normalized.publishKey,
			}); lookupErr == nil {
				h.respondToPublishReplay(w, existing, normalized.requestDigest)
				return db.DevReport{}, false
			}
			writeError(w, http.StatusConflict, "dev report publish conflict, retry the request")
			return db.DevReport{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to publish dev report")
		return db.DevReport{}, false
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit dev report")
		return db.DevReport{}, false
	}
	return report, true
}

func (h *Handler) normalizeDevReportRequest(w http.ResponseWriter, r *http.Request, req *PublishDevReportRequest) (normalizedDevReportRequest, bool) {
	if req.SchemaVersion != devReportSchemaVersion {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported schema_version: %d", req.SchemaVersion))
		return normalizedDevReportRequest{}, false
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return normalizedDevReportRequest{}, false
	}
	if len(version) > devReportMaxVersionLen {
		writeError(w, http.StatusBadRequest, "version is too long")
		return normalizedDevReportRequest{}, false
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return normalizedDevReportRequest{}, false
	}
	if len(title) > devReportMaxTitleLen {
		writeError(w, http.StatusBadRequest, "title is too long")
		return normalizedDevReportRequest{}, false
	}

	metricVersion := strings.TrimSpace(req.MetricVersion)
	if metricVersion == "" {
		writeError(w, http.StatusBadRequest, "metric_version is required")
		return normalizedDevReportRequest{}, false
	}
	if len(metricVersion) > devReportMaxMetricLen {
		writeError(w, http.StatusBadRequest, "metric_version is too long")
		return normalizedDevReportRequest{}, false
	}

	publishKey := strings.TrimSpace(req.PublishKey)
	if publishKey == "" {
		writeError(w, http.StatusBadRequest, "publish_key is required")
		return normalizedDevReportRequest{}, false
	}
	if len(publishKey) > devReportMaxPublishKeyLen {
		writeError(w, http.StatusBadRequest, "publish_key is too long")
		return normalizedDevReportRequest{}, false
	}

	if req.BodyMarkdown == "" {
		writeError(w, http.StatusBadRequest, "body_markdown is required")
		return normalizedDevReportRequest{}, false
	}

	// RFC3339 requires an explicit offset (Z or ±hh:mm) — a naive timestamp is
	// rejected rather than guessed at in server-local time.
	observation, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ObservationAsOf))
	if err != nil {
		writeError(w, http.StatusBadRequest, "observation_as_of must be RFC3339 with a timezone")
		return normalizedDevReportRequest{}, false
	}

	var comparisonVersion pgtype.Text
	if req.ComparisonVersion != nil {
		trimmed := strings.TrimSpace(*req.ComparisonVersion)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "comparison_version cannot be empty when provided")
			return normalizedDevReportRequest{}, false
		}
		if len(trimmed) > devReportMaxVersionLen {
			writeError(w, http.StatusBadRequest, "comparison_version is too long")
			return normalizedDevReportRequest{}, false
		}
		comparisonVersion = pgtype.Text{String: trimmed, Valid: true}
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return normalizedDevReportRequest{}, false
	}

	var projectUUID pgtype.UUID
	var projectIDString *string
	if req.ProjectID != nil {
		trimmed := strings.TrimSpace(*req.ProjectID)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "project_id cannot be empty when provided")
			return normalizedDevReportRequest{}, false
		}
		parsed, ok := parseUUIDOrBadRequest(w, trimmed, "project_id")
		if !ok {
			return normalizedDevReportRequest{}, false
		}
		// A report may only reference a project in the same workspace; this
		// also proves the project exists before the insert.
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          parsed,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project_id does not belong to this workspace")
			return normalizedDevReportRequest{}, false
		}
		projectUUID = parsed
		idStr := uuidToString(parsed)
		projectIDString = &idStr
	}

	bodySum := sha256.Sum256([]byte(req.BodyMarkdown))
	bodySHA := hex.EncodeToString(bodySum[:])

	digestInput := devReportDigestPayload{
		SchemaVersion:     req.SchemaVersion,
		Version:           version,
		ComparisonVersion: textToPtr(comparisonVersion),
		ProjectID:         projectIDString,
		Title:             title,
		MetricVersion:     metricVersion,
		ObservationAsOf:   observation.UTC().Format(time.RFC3339),
		BodySHA256:        bodySHA,
	}
	digestBytes, err := json.Marshal(digestInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to digest request")
		return normalizedDevReportRequest{}, false
	}
	digestSum := sha256.Sum256(digestBytes)

	return normalizedDevReportRequest{
		version:           version,
		comparisonVersion: comparisonVersion,
		projectID:         projectUUID,
		projectIDString:   projectIDString,
		title:             title,
		metricVersion:     metricVersion,
		observationAsOf:   pgtype.Timestamptz{Time: observation.UTC(), Valid: true},
		bodyMarkdown:      req.BodyMarkdown,
		bodySHA256:        bodySHA,
		publishKey:        publishKey,
		requestDigest:     hex.EncodeToString(digestSum[:]),
	}, true
}

// parseBoundedPagination reads limit/offset with sane defaults and clamps the
// limit so a client cannot turn the list endpoint into a full-table scan.
func parseBoundedPagination(r *http.Request, defaultLimit, maxLimit int32) (int32, int32) {
	limit := defaultLimit
	offset := int32(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := parsePositiveInt(raw); err == nil {
			limit = int32(parsed)
		}
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := parsePositiveInt(raw); err == nil {
			offset = int32(parsed)
		}
	}
	return limit, offset
}

func parsePositiveInt(raw string) (int, error) {
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, errors.New("not a non-negative integer")
		}
		value = value*10 + int(ch-'0')
		if value > 1_000_000 {
			return 0, errors.New("out of range")
		}
	}
	if raw == "" {
		return 0, errors.New("empty")
	}
	return value, nil
}

func sanitizeFilenamePart(value string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, value)
	mapped = strings.Trim(mapped, "-.")
	if mapped == "" {
		return "report"
	}
	return mapped
}
