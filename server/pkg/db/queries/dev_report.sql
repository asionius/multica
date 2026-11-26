-- name: ListLatestDevReports :many
-- One row per version: its highest revision. Metadata only; body_markdown is
-- fetched by the detail endpoint so list responses stay small.
SELECT id, workspace_id, project_id, version, comparison_version, title,
       schema_version, metric_version, observation_as_of, revision,
       body_sha256, published_by, published_at
FROM (
    SELECT d.*,
           row_number() OVER (PARTITION BY d.version ORDER BY d.revision DESC) AS rn
    FROM dev_report d
    WHERE d.workspace_id = $1
      AND (sqlc.narg('version')::text IS NULL OR d.version = sqlc.narg('version'))
) ranked
WHERE rn = 1
ORDER BY observation_as_of DESC, version DESC, revision DESC
LIMIT $2::int OFFSET $3::int;

-- name: CountLatestDevReports :one
SELECT count(*) FROM (
    SELECT DISTINCT d.version
    FROM dev_report d
    WHERE d.workspace_id = $1
      AND (sqlc.narg('version')::text IS NULL OR d.version = sqlc.narg('version'))
) versions;

-- name: GetDevReportInWorkspace :one
SELECT * FROM dev_report
WHERE id = $1 AND workspace_id = $2;

-- name: GetDevReportByPublishKey :one
SELECT * FROM dev_report
WHERE workspace_id = $1 AND publish_key = $2;

-- name: ListDevReportRevisions :many
SELECT id, workspace_id, project_id, version, comparison_version, title,
       schema_version, metric_version, observation_as_of, revision,
       body_sha256, published_by, published_at
FROM dev_report
WHERE workspace_id = $1 AND version = $2
ORDER BY revision DESC
LIMIT $3::int OFFSET $4::int;

-- name: LockDevReportVersion :exec
-- Serializes revision allocation per (workspace, version) for the duration of
-- the publishing transaction. Released automatically on commit/rollback.
SELECT pg_advisory_xact_lock(hashtextextended($1::text || ':' || $2::text, 0));

-- name: GetLatestDevReportRevision :one
SELECT revision FROM dev_report
WHERE workspace_id = $1 AND version = $2
ORDER BY revision DESC
LIMIT 1;

-- name: CreateDevReport :one
INSERT INTO dev_report (
    workspace_id, project_id, version, comparison_version, title,
    schema_version, metric_version, observation_as_of, revision,
    body_markdown, body_sha256, publish_key, request_digest, published_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: CountDevReportRevisions :one
SELECT count(*) FROM dev_report
WHERE workspace_id = $1 AND version = $2;

