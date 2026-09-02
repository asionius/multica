CREATE TABLE session_diagnostic (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id    UUID REFERENCES workspace(id) ON DELETE SET NULL,
    session_id      TEXT NOT NULL DEFAULT '',
    hostname        TEXT NOT NULL DEFAULT '',
    plugin_version  TEXT NOT NULL DEFAULT '',
    filename        TEXT NOT NULL,
    url             TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'application/gzip',
    size_bytes      BIGINT NOT NULL,
    file_manifest   JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_diagnostic_created ON session_diagnostic(created_at DESC);
CREATE INDEX idx_session_diagnostic_user_created ON session_diagnostic(user_id, created_at DESC);
