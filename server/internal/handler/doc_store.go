package handler

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Personal document store ("multica store"). Redis-like key/value semantics
// for a single allow-listed account: the value is an opaque blob (xlsx, md,
// csv, ...), keys are user-chosen and prefix-listable.
const (
	maxDocStoreValueBytes = 25 << 20 // 25 MiB — big enough for a week of spreadsheets
	maxDocStoreKeyBytes   = 512
	docStoreDefaultLimit  = 100
)

// defaultDocStoreAllowedEmails keeps the store private to the account that
// asked for it. DOC_STORE_ALLOWED_EMAILS replaces (not merges with) this list.
var defaultDocStoreAllowedEmails = []string{"shuaijin@tencent.com"}

// DocStoreAllowedEmailsFromEnv parses DOC_STORE_ALLOWED_EMAILS (comma
// separated). Empty / unset falls back to the built-in single-user default.
func DocStoreAllowedEmailsFromEnv(raw string) []string {
	if emails := splitCommaList(raw); len(emails) > 0 {
		return emails
	}
	out := make([]string, len(defaultDocStoreAllowedEmails))
	copy(out, defaultDocStoreAllowedEmails)
	return out
}

type DocStoreEntryResponse struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ListDocStoreResponse struct {
	Keys  []DocStoreEntryResponse `json:"keys"`
	Total int64                   `json:"total"`
}

func docStoreEntryFromRow(row db.ListDocStoreKeysRow) DocStoreEntryResponse {
	return DocStoreEntryResponse{
		Key:         row.Key,
		ContentType: row.ContentType,
		SizeBytes:   row.SizeBytes,
		CreatedAt:   timestampToString(row.CreatedAt),
		UpdatedAt:   timestampToString(row.UpdatedAt),
	}
}

func (h *Handler) docStoreEmailAllowed(email string) bool {
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return false
	}
	for _, allowed := range h.cfg.DocStoreAllowedEmails {
		if strings.ToLower(strings.TrimSpace(allowed)) == target {
			return true
		}
	}
	return false
}

// requireDocStoreUser authenticates the caller and enforces the email
// allow-list. Non-allow-listed accounts get 403 — the store is not a general
// product surface.
func (h *Handler) requireDocStoreUser(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, false
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return pgtype.UUID{}, false
	}
	user, err := h.Queries.GetUser(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	if !h.docStoreEmailAllowed(user.Email) {
		writeError(w, http.StatusForbidden, "doc store is not enabled for this account")
		return pgtype.UUID{}, false
	}
	return userUUID, true
}

// normalizeDocStoreKey rejects keys that cannot be addressed reliably by the
// CLI or that would be ambiguous as path segments.
func normalizeDocStoreKey(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("key is required")
	}
	if len(raw) > maxDocStoreKeyBytes {
		return "", errors.New("key is too long")
	}
	if strings.HasPrefix(raw, "/") {
		return "", errors.New("key must not start with a slash")
	}
	if strings.TrimSpace(raw) != raw {
		return "", errors.New("key must not have leading or trailing whitespace")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("key must not contain control characters")
		}
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("key must not contain empty, dot or dot-dot segments")
		}
	}
	return raw, nil
}

func docStoreKeyFromURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := chi.URLParam(r, "*")
	key, err := normalizeDocStoreKey(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return key, true
}

func normalizeDocStoreContentType(raw string) (string, bool) {
	ct := strings.TrimSpace(raw)
	if ct == "" {
		return "application/octet-stream", true
	}
	if len(ct) > 255 || strings.ContainsAny(ct, "\r\n") {
		return "", false
	}
	return ct, true
}

// PutDocStoreValue — PUT /api/doc-store/* (key is the wildcard path).
func (h *Handler) PutDocStoreValue(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := h.requireDocStoreUser(w, r)
	if !ok {
		return
	}
	key, ok := docStoreKeyFromURL(w, r)
	if !ok {
		return
	}
	contentType, ok := normalizeDocStoreContentType(r.Header.Get("Content-Type"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid content type")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDocStoreValueBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "document exceeds the 25 MiB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	entry, err := h.Queries.UpsertDocStoreValue(r.Context(), db.UpsertDocStoreValueParams{
		OwnerUserID: userUUID,
		Key:         key,
		Value:       data,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store document")
		return
	}

	w.Header().Set("X-Doc-Key", entry.Key)
	w.Header().Set("X-Doc-Updated-At", timestampToString(entry.UpdatedAt))
	writeJSON(w, http.StatusOK, DocStoreEntryResponse{
		Key:         entry.Key,
		ContentType: entry.ContentType,
		SizeBytes:   entry.SizeBytes,
		CreatedAt:   timestampToString(entry.CreatedAt),
		UpdatedAt:   timestampToString(entry.UpdatedAt),
	})
}

// GetDocStoreValue — GET /api/doc-store/* returns the raw stored bytes.
func (h *Handler) GetDocStoreValue(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := h.requireDocStoreUser(w, r)
	if !ok {
		return
	}
	key, ok := docStoreKeyFromURL(w, r)
	if !ok {
		return
	}

	entry, err := h.Queries.GetDocStoreValue(r.Context(), db.GetDocStoreValueParams{
		OwnerUserID: userUUID,
		Key:         key,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	filename := sanitizeFilenamePart(path.Base(entry.Key))
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("X-Doc-Key", entry.Key)
	w.Header().Set("X-Doc-Updated-At", timestampToString(entry.UpdatedAt))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Value)
}

// DeleteDocStoreValue — DELETE /api/doc-store/*.
func (h *Handler) DeleteDocStoreValue(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := h.requireDocStoreUser(w, r)
	if !ok {
		return
	}
	key, ok := docStoreKeyFromURL(w, r)
	if !ok {
		return
	}
	affected, err := h.Queries.DeleteDocStoreValue(r.Context(), db.DeleteDocStoreValueParams{
		OwnerUserID: userUUID,
		Key:         key,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete document")
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListDocStoreKeys — GET /api/doc-store?prefix=&limit=&offset=.
func (h *Handler) ListDocStoreKeys(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := h.requireDocStoreUser(w, r)
	if !ok {
		return
	}

	var prefix pgtype.Text
	if raw := r.URL.Query().Get("prefix"); raw != "" {
		if len(raw) > maxDocStoreKeyBytes {
			writeError(w, http.StatusBadRequest, "prefix is too long")
			return
		}
		prefix = pgtype.Text{String: raw, Valid: true}
	}

	limit, offset := parseBoundedPagination(r, docStoreDefaultLimit, 500)
	rows, err := h.Queries.ListDocStoreKeys(r.Context(), db.ListDocStoreKeysParams{
		OwnerUserID: userUUID,
		Prefix:      prefix,
		Column2:     limit,
		Column3:     offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	total, err := h.Queries.CountDocStoreKeys(r.Context(), db.CountDocStoreKeysParams{
		OwnerUserID: userUUID,
		Prefix:      prefix,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count documents")
		return
	}

	keys := make([]DocStoreEntryResponse, len(rows))
	for i, row := range rows {
		keys[i] = docStoreEntryFromRow(row)
	}
	writeJSON(w, http.StatusOK, ListDocStoreResponse{Keys: keys, Total: total})
}
