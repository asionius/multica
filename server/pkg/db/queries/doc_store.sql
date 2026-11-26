-- name: UpsertDocStoreValue :one
INSERT INTO doc_store (owner_user_id, key, value, content_type, size_bytes)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (owner_user_id, key) DO UPDATE SET
    value = EXCLUDED.value,
    content_type = EXCLUDED.content_type,
    size_bytes = EXCLUDED.size_bytes,
    updated_at = now()
RETURNING *;

-- name: GetDocStoreValue :one
SELECT * FROM doc_store
WHERE owner_user_id = $1 AND key = $2;

-- name: ListDocStoreKeys :many
SELECT key, content_type, size_bytes, created_at, updated_at
FROM doc_store
WHERE owner_user_id = $1
  AND (sqlc.narg('prefix')::text IS NULL OR key LIKE sqlc.narg('prefix') || '%')
ORDER BY key
LIMIT $2::int OFFSET $3::int;

-- name: CountDocStoreKeys :one
SELECT count(*) FROM doc_store
WHERE owner_user_id = $1
  AND (sqlc.narg('prefix')::text IS NULL OR key LIKE sqlc.narg('prefix') || '%');

-- name: DeleteDocStoreValue :execrows
DELETE FROM doc_store
WHERE owner_user_id = $1 AND key = $2;
