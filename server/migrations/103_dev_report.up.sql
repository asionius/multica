CREATE TABLE dev_report (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id         UUID REFERENCES project(id) ON DELETE SET NULL,
    version            TEXT NOT NULL,
    comparison_version TEXT,
    title              TEXT NOT NULL,
    schema_version     INT NOT NULL DEFAULT 1,
    metric_version     TEXT NOT NULL,
    observation_as_of  TIMESTAMPTZ NOT NULL,
    revision           INT NOT NULL,
    body_markdown      TEXT NOT NULL,
    body_sha256        TEXT NOT NULL,
    publish_key        TEXT NOT NULL,
    request_digest     TEXT NOT NULL,
    -- No FK on purpose: deleting a user must never delete an already-published
    -- report revision. The UUID is kept as an audit pointer only.
    published_by       UUID NOT NULL,
    published_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT dev_report_title_len CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT dev_report_version_len CHECK (char_length(version) BETWEEN 1 AND 64),
    CONSTRAINT dev_report_comparison_version_len
        CHECK (comparison_version IS NULL OR char_length(comparison_version) BETWEEN 1 AND 64),
    CONSTRAINT dev_report_metric_version_len CHECK (char_length(metric_version) BETWEEN 1 AND 200),
    CONSTRAINT dev_report_publish_key_len CHECK (char_length(publish_key) BETWEEN 1 AND 128),
    CONSTRAINT dev_report_schema_version_positive CHECK (schema_version >= 1),
    CONSTRAINT dev_report_revision_positive CHECK (revision >= 1),
    CONSTRAINT dev_report_body_nonempty CHECK (char_length(body_markdown) > 0),

    CONSTRAINT dev_report_workspace_version_revision_key
        UNIQUE (workspace_id, version, revision),
    CONSTRAINT dev_report_workspace_publish_key_key
        UNIQUE (workspace_id, publish_key)
);

-- List path: latest revision per version inside one workspace.
CREATE INDEX idx_dev_report_workspace_version_revision
    ON dev_report (workspace_id, version, revision DESC);

-- List ordering / pagination by observation time.
CREATE INDEX idx_dev_report_workspace_observation
    ON dev_report (workspace_id, observation_as_of DESC, revision DESC);

-- Optional project scoping (project deletion sets this to NULL, report survives).
CREATE INDEX idx_dev_report_project ON dev_report (project_id);
