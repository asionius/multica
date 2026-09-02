package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	diagnosticHourlyRateLimit = 10
	diagnosticDefaultPublic   = "https://tmultica.woa.com"
	diagnosticContentType     = "application/gzip"
)

var (
	errDiagnosticTraversal = errors.New("archive contains a path that escapes the root")
	errDiagnosticSymlink   = errors.New("archive must not contain symbolic or hard links")
	errDiagnosticArchive   = errors.New("invalid tar.gz archive")
	errDiagnosticNotInTar  = errors.New("path not found in archive")
)

// defaultDiagnosticsAdminEmails is used when DIAGNOSTICS_ADMIN_EMAILS is unset.
// Override by setting the env to a comma-separated list (replaces this
// default; it does not merge).
var defaultDiagnosticsAdminEmails = []string{
	"aikewang@tencent.com",
}

// DiagnosticsAdminEmailsFromEnv parses DIAGNOSTICS_ADMIN_EMAILS. The value
// is a comma-separated list and may contain multiple addresses. Whitespace
// around entries is ignored. An empty / unset env falls back to
// defaultDiagnosticsAdminEmails.
func DiagnosticsAdminEmailsFromEnv(raw string) []string {
	if emails := splitCommaList(raw); len(emails) > 0 {
		return emails
	}
	if len(defaultDiagnosticsAdminEmails) == 0 {
		return nil
	}
	out := make([]string, len(defaultDiagnosticsAdminEmails))
	copy(out, defaultDiagnosticsAdminEmails)
	return out
}

func splitCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type diagnosticFileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type CreateSessionDiagnosticResponse struct {
	ID          string `json:"id"`
	ViewURL     string `json:"view_url"`
	DownloadURL string `json:"download_url"`
}

type SessionDiagnosticResponse struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	Hostname      string          `json:"hostname"`
	PluginVersion string          `json:"plugin_version"`
	Filename      string          `json:"filename"`
	SizeBytes     int64           `json:"size_bytes"`
	FileCount     int             `json:"file_count"`
	FileManifest  json.RawMessage `json:"file_manifest"`
	Metadata      json.RawMessage `json:"metadata"`
	UploaderName  string          `json:"uploader_name"`
	UploaderEmail string          `json:"uploader_email"`
	CreatedAt     string          `json:"created_at"`
	ViewURL       string          `json:"view_url"`
	DownloadURL   string          `json:"download_url"`
}

type ListSessionDiagnosticsResponse struct {
	Diagnostics []SessionDiagnosticResponse `json:"diagnostics"`
}

func (h *Handler) diagnosticsPublicBase() string {
	if h.cfg.PublicURL != "" {
		return strings.TrimRight(h.cfg.PublicURL, "/")
	}
	return diagnosticDefaultPublic
}

func (h *Handler) diagnosticURLs(id string) (viewURL, downloadURL string) {
	base := h.diagnosticsPublicBase()
	return base + "/diagnostics/" + id, base + "/api/diagnostics/" + id + "/download"
}

func (h *Handler) isDiagnosticsAdmin(ctxUserID string, r *http.Request) bool {
	if len(h.cfg.DiagnosticsAdminEmails) == 0 {
		return false
	}
	user, err := h.Queries.GetUser(r.Context(), parseUUID(ctxUserID))
	if err != nil {
		return false
	}
	email := strings.TrimSpace(user.Email)
	for _, allowed := range h.cfg.DiagnosticsAdminEmails {
		if strings.EqualFold(email, allowed) {
			return true
		}
	}
	return false
}

func (h *Handler) canAccessDiagnostic(userID string, ownerID pgtype.UUID, r *http.Request) bool {
	if uuidToString(ownerID) == userID {
		return true
	}
	return h.isDiagnosticsAdmin(userID, r)
}

func (h *Handler) loadDiagnosticForUser(w http.ResponseWriter, r *http.Request) (db.SessionDiagnostic, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.SessionDiagnostic{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "diagnostic id")
	if !ok {
		return db.SessionDiagnostic{}, false
	}
	row, err := h.Queries.GetSessionDiagnostic(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "diagnostic not found")
		return db.SessionDiagnostic{}, false
	}
	if !h.canAccessDiagnostic(userID, row.UserID, r) {
		writeError(w, http.StatusForbidden, "not authorized to view this diagnostic")
		return db.SessionDiagnostic{}, false
	}
	return row, true
}

// CreateSessionDiagnostic — POST /api/diagnostics
func (h *Handler) CreateSessionDiagnostic(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	count, err := h.Queries.CountRecentSessionDiagnosticsByUser(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("count recent diagnostics failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to check rate limit")
		return
	}
	if count >= diagnosticHourlyRateLimit {
		writeError(w, http.StatusTooManyRequests, "too many diagnostic uploads, please try again later")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "file is empty")
		return
	}

	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	manifest, err := inspectDiagnosticArchive(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode file manifest")
		return
	}

	metadata := map[string]any{}
	if note := strings.TrimSpace(r.FormValue("note")); note != "" {
		metadata["note"] = note
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		metaBytes = []byte("{}")
	}

	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate diagnostic uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	filename := path.Base(header.Filename)
	if filename == "" || filename == "." || filename == "/" {
		filename = "session.tar.gz"
	}

	key := "diagnostics/" + id.String() + ".tar.gz"
	link, err := h.Storage.Upload(r.Context(), key, data, diagnosticContentType, filename)
	if err != nil {
		slog.Error("diagnostic upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}

	var workspaceID pgtype.UUID
	if ws := h.resolveWorkspaceID(r); ws != "" {
		if parsed, ok := parseUUIDOrBadRequest(w, ws, "workspace_id"); ok {
			workspaceID = parsed
		} else {
			return
		}
	}

	row, err := h.Queries.CreateSessionDiagnostic(r.Context(), db.CreateSessionDiagnosticParams{
		ID:            pgtype.UUID{Bytes: id, Valid: true},
		UserID:        parseUUID(userID),
		WorkspaceID:   workspaceID,
		SessionID:     sessionID,
		Hostname:      strings.TrimSpace(r.FormValue("hostname")),
		PluginVersion: strings.TrimSpace(r.FormValue("plugin_version")),
		Filename:      filename,
		Url:           link,
		ContentType:   diagnosticContentType,
		SizeBytes:     int64(len(data)),
		FileManifest:  manifestBytes,
		Metadata:      metaBytes,
	})
	if err != nil {
		slog.Warn("create session diagnostic failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save diagnostic")
		return
	}

	diagID := uuidToString(row.ID)
	viewURL, downloadURL := h.diagnosticURLs(diagID)
	slog.Info("session diagnostic uploaded", append(logger.RequestAttrs(r), "diagnostic_id", diagID)...)
	writeJSON(w, http.StatusCreated, CreateSessionDiagnosticResponse{
		ID:          diagID,
		ViewURL:     viewURL,
		DownloadURL: downloadURL,
	})
}

// ListSessionDiagnostics — GET /api/diagnostics
func (h *Handler) ListSessionDiagnostics(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var (
		items []SessionDiagnosticResponse
		err   error
	)
	if h.isDiagnosticsAdmin(userID, r) {
		rows, qerr := h.Queries.ListSessionDiagnostics(r.Context())
		err = qerr
		items = make([]SessionDiagnosticResponse, 0, len(rows))
		for _, row := range rows {
			items = append(items, h.listRowToResponse(row.ID, row.SessionID, row.Hostname, row.PluginVersion, row.Filename, row.SizeBytes, row.FileManifest, row.Metadata, row.UploaderName, row.UploaderEmail, row.CreatedAt))
		}
	} else {
		rows, qerr := h.Queries.ListSessionDiagnosticsByUser(r.Context(), parseUUID(userID))
		err = qerr
		items = make([]SessionDiagnosticResponse, 0, len(rows))
		for _, row := range rows {
			items = append(items, h.listRowToResponse(row.ID, row.SessionID, row.Hostname, row.PluginVersion, row.Filename, row.SizeBytes, row.FileManifest, row.Metadata, row.UploaderName, row.UploaderEmail, row.CreatedAt))
		}
	}
	if err != nil {
		slog.Warn("list session diagnostics failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list diagnostics")
		return
	}
	if items == nil {
		items = []SessionDiagnosticResponse{}
	}
	writeJSON(w, http.StatusOK, ListSessionDiagnosticsResponse{Diagnostics: items})
}

// GetSessionDiagnostic — GET /api/diagnostics/{id}
func (h *Handler) GetSessionDiagnostic(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadDiagnosticForUser(w, r)
	if !ok {
		return
	}
	name, email := h.uploaderIdentity(r, row.UserID)
	writeJSON(w, http.StatusOK, h.listRowToResponse(row.ID, row.SessionID, row.Hostname, row.PluginVersion, row.Filename, row.SizeBytes, row.FileManifest, row.Metadata, name, email, row.CreatedAt))
}

// DownloadSessionDiagnostic — GET /api/diagnostics/{id}/download
func (h *Handler) DownloadSessionDiagnostic(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadDiagnosticForUser(w, r)
	if !ok {
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}
	key := h.Storage.KeyFromURL(row.Url)
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open diagnostic archive", "id", uuidToString(row.ID), "key", key, "error", err)
		writeError(w, http.StatusNotFound, "diagnostic object not found")
		return
	}
	defer reader.Close()

	filename := row.Filename
	if filename == "" {
		filename = "session.tar.gz"
	}
	w.Header().Set("Content-Type", row.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if row.SizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", row.SizeBytes))
	}
	if _, err := io.Copy(w, reader); err != nil {
		slog.Error("failed to write diagnostic download", "id", uuidToString(row.ID), "error", err)
	}
}

// GetSessionDiagnosticFile — GET /api/diagnostics/{id}/files?path=
func (h *Handler) GetSessionDiagnosticFile(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadDiagnosticForUser(w, r)
	if !ok {
		return
	}
	wantPath, err := sanitizeDiagnosticPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}
	key := h.Storage.KeyFromURL(row.Url)
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open diagnostic archive for preview", "id", uuidToString(row.ID), "key", key, "error", err)
		writeError(w, http.StatusNotFound, "diagnostic object not found")
		return
	}
	defer reader.Close()

	body, err := extractDiagnosticFile(reader, wantPath)
	if err != nil {
		switch {
		case errors.Is(err, errDiagnosticNotInTar):
			writeError(w, http.StatusNotFound, "file not found in archive")
		case errors.Is(err, errDiagnosticTraversal), errors.Is(err, errDiagnosticSymlink):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			if errors.Is(err, errPreviewTooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "file too large for inline preview")
				return
			}
			writeError(w, http.StatusBadRequest, "failed to extract file")
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Original-Content-Type", previewContentType(wantPath))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write diagnostic file preview", "id", uuidToString(row.ID), "error", err)
	}
}

var errPreviewTooLarge = errors.New("file too large for inline preview")

func (h *Handler) uploaderIdentity(r *http.Request, userID pgtype.UUID) (name, email string) {
	user, err := h.Queries.GetUser(r.Context(), userID)
	if err != nil {
		return "", ""
	}
	return user.Name, user.Email
}

func (h *Handler) listRowToResponse(
	id pgtype.UUID,
	sessionID, hostname, pluginVersion, filename string,
	sizeBytes int64,
	manifest, metadata []byte,
	uploaderName, uploaderEmail string,
	createdAt pgtype.Timestamptz,
) SessionDiagnosticResponse {
	diagID := uuidToString(id)
	viewURL, downloadURL := h.diagnosticURLs(diagID)
	return SessionDiagnosticResponse{
		ID:            diagID,
		SessionID:     sessionID,
		Hostname:      hostname,
		PluginVersion: pluginVersion,
		Filename:      filename,
		SizeBytes:     sizeBytes,
		FileCount:     diagnosticFileCount(manifest),
		FileManifest:  rawJSONOr(manifest, "[]"),
		Metadata:      rawJSONOr(metadata, "{}"),
		UploaderName:  uploaderName,
		UploaderEmail: uploaderEmail,
		CreatedAt:     timestampToString(createdAt),
		ViewURL:       viewURL,
		DownloadURL:   downloadURL,
	}
}

func rawJSONOr(b []byte, fallback string) json.RawMessage {
	if len(bytes.TrimSpace(b)) == 0 {
		return json.RawMessage(fallback)
	}
	if !json.Valid(b) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(b)
}

func diagnosticFileCount(manifest []byte) int {
	var files []diagnosticFileEntry
	if err := json.Unmarshal(manifest, &files); err != nil {
		return 0
	}
	return len(files)
}

func previewContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return "application/json"
	case ".jsonl":
		return "application/x-ndjson"
	case ".md", ".markdown":
		return "text/markdown"
	default:
		return "text/plain"
	}
}

func sanitizeDiagnosticPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("path is required")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return "", errDiagnosticTraversal
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "..") {
		return "", errDiagnosticTraversal
	}
	return cleaned, nil
}

func inspectDiagnosticArchive(data []byte) ([]diagnosticFileEntry, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errDiagnosticArchive
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var files []diagnosticFileEntry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errDiagnosticArchive
		}
		if err := rejectUnsafeTarHeader(hdr); err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name, err := sanitizeDiagnosticPath(hdr.Name)
		if err != nil {
			return nil, errDiagnosticTraversal
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, errDiagnosticArchive
		}
		files = append(files, diagnosticFileEntry{Path: name, Size: hdr.Size})
	}
	if files == nil {
		files = []diagnosticFileEntry{}
	}
	return files, nil
}

func extractDiagnosticFile(r io.Reader, wantPath string) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, errDiagnosticArchive
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, errDiagnosticNotInTar
		}
		if err != nil {
			return nil, errDiagnosticArchive
		}
		if err := rejectUnsafeTarHeader(hdr); err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name, err := sanitizeDiagnosticPath(hdr.Name)
		if err != nil {
			return nil, errDiagnosticTraversal
		}
		if name != wantPath {
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return nil, errDiagnosticArchive
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxPreviewTextSize+1))
		if err != nil {
			return nil, errDiagnosticArchive
		}
		if len(body) > maxPreviewTextSize {
			return nil, errPreviewTooLarge
		}
		return body, nil
	}
}

func rejectUnsafeTarHeader(hdr *tar.Header) error {
	if hdr == nil {
		return errDiagnosticArchive
	}
	switch hdr.Typeflag {
	case tar.TypeSymlink, tar.TypeLink:
		return errDiagnosticSymlink
	}
	name := strings.ReplaceAll(hdr.Name, "\\", "/")
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.HasPrefix(path.Clean(name), "..") {
		return errDiagnosticTraversal
	}
	if hdr.Linkname != "" {
		link := strings.ReplaceAll(hdr.Linkname, "\\", "/")
		if strings.Contains(link, "..") || strings.HasPrefix(link, "/") {
			return errDiagnosticTraversal
		}
	}
	return nil
}
