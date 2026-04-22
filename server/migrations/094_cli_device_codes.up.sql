-- Device authorization flow for CLI login (RFC 8628 style).
-- Instead of opening a local HTTP callback server, the CLI posts to
-- /api/cli/device/start to obtain a short user_code, the user types it
-- into the /cli/verify page in a browser, approves, and the CLI polls
-- /api/cli/device/poll with the device_code until a PAT is returned.
CREATE TABLE cli_device_code (
    device_code  TEXT        PRIMARY KEY,
    user_code    TEXT        NOT NULL UNIQUE,
    hostname     TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL DEFAULT 'pending',
    user_id      UUID        REFERENCES "user"(id) ON DELETE CASCADE,
    token        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    approved_at  TIMESTAMPTZ,

    CONSTRAINT cli_device_code_status_check
        CHECK (status IN ('pending', 'approved', 'denied', 'expired'))
);

CREATE INDEX idx_cli_device_code_user_code ON cli_device_code(user_code);
CREATE INDEX idx_cli_device_code_expires   ON cli_device_code(expires_at);
