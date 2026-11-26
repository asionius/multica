-- Personal key/value document store, exposed through the CLI "multica store"
-- commands. Redis-like semantics (SET/GET/DEL/KEYS), durable and scoped to a
-- single user; no workspace membership is required.
CREATE TABLE doc_store (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    value         BYTEA NOT NULL,
    content_type  TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT doc_store_key_len CHECK (char_length(key) BETWEEN 1 AND 512),
    CONSTRAINT doc_store_owner_key_key UNIQUE (owner_user_id, key)
);

CREATE INDEX idx_doc_store_owner_key ON doc_store (owner_user_id, key text_pattern_ops);
