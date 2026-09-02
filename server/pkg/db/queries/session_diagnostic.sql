-- name: CreateSessionDiagnostic :one
INSERT INTO session_diagnostic (
    id, user_id, workspace_id, session_id, hostname, plugin_version,
    filename, url, content_type, size_bytes, file_manifest, metadata
)
VALUES ($1, $2, sqlc.narg(workspace_id), $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetSessionDiagnostic :one
SELECT * FROM session_diagnostic
WHERE id = $1;

-- name: ListSessionDiagnostics :many
SELECT sd.*,
       u.name  AS uploader_name,
       u.email AS uploader_email
FROM session_diagnostic sd
JOIN "user" u ON u.id = sd.user_id
ORDER BY sd.created_at DESC;

-- name: ListSessionDiagnosticsByUser :many
SELECT sd.*,
       u.name  AS uploader_name,
       u.email AS uploader_email
FROM session_diagnostic sd
JOIN "user" u ON u.id = sd.user_id
WHERE sd.user_id = $1
ORDER BY sd.created_at DESC;

-- name: CountRecentSessionDiagnosticsByUser :one
SELECT count(*) FROM session_diagnostic
WHERE user_id = $1 AND created_at > now() - interval '1 hour';
