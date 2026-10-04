-- How far each OpenIPC/builder device has drifted from OpenIPC/firmware,
-- pushed once a day by builder's firmware-drift.yml over its OIDC token
-- (internal/drift/PUSH.md). It used to be the text of one GitHub issue
-- (OpenIPC/builder#131); here every finding is attributed to the devices it
-- reaches, and keeping the last reports says how long each has stood.
--
-- Rows, not documents, like the builds tables: the report arrives as JSON
-- and is parsed into these; no JSON is kept.

CREATE TABLE drift_reports (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    checked_at         timestamptz NOT NULL,
    builder_commit     text NOT NULL CHECK (builder_commit ~ '^[0-9a-f]{40}$'),
    firmware_commit    text NOT NULL CHECK (firmware_commit ~ '^[0-9a-f]{40}$'),
    buildroot_version  text NOT NULL,
    run_url            text NOT NULL,
    -- The run that pushed it, from the verified token. A retried push of the
    -- same run attempt replaces its report rather than adding a second.
    pushed_by          text NOT NULL UNIQUE,
    received_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX drift_reports_newest ON drift_reports (checked_at DESC, id DESC);

-- Every device builder builds, so a device with nothing to report is listed
-- as in step rather than missing.
CREATE TABLE drift_devices (
    report_id  bigint NOT NULL REFERENCES drift_reports (id) ON DELETE CASCADE,
    device     text   NOT NULL,
    dir        text   NOT NULL,
    PRIMARY KEY (report_id, device)
);

-- A file under devices/ that builder.sh copies over the firmware file of the
-- same (or a hand-mapped) path, and where that stands.
CREATE TABLE drift_shadows (
    report_id      bigint NOT NULL REFERENCES drift_reports (id) ON DELETE CASCADE,
    builder_path   text   NOT NULL,
    firmware_path  text   NOT NULL,
    status         text   NOT NULL CHECK (status IN ('ok', 'unpinned', 'missing_builder', 'firmware_gone', 'moved')),
    pinned_blob    text,
    current_blob   text,
    pinned_commit  text,
    pin_unknown    boolean NOT NULL DEFAULT false,
    truncated      boolean NOT NULL DEFAULT false,
    reconciled     date,
    note           text,
    PRIMARY KEY (report_id, builder_path)
);

CREATE TABLE drift_shadow_devices (
    report_id     bigint NOT NULL,
    builder_path  text   NOT NULL,
    device        text   NOT NULL,
    PRIMARY KEY (report_id, builder_path, device),
    FOREIGN KEY (report_id, builder_path) REFERENCES drift_shadows (report_id, builder_path) ON DELETE CASCADE
);
CREATE INDEX drift_shadow_devices_by_device ON drift_shadow_devices (device, report_id);

-- The firmware commits that touched a moved file after the one its pin names.
CREATE TABLE drift_commits (
    report_id     bigint NOT NULL,
    builder_path  text   NOT NULL,
    position      int    NOT NULL,
    sha           text   NOT NULL CHECK (sha ~ '^[0-9a-f]{40}$'),
    committed_at  timestamptz NOT NULL,
    author        text   NOT NULL,
    subject       text   NOT NULL,
    PRIMARY KEY (report_id, builder_path, position),
    FOREIGN KEY (report_id, builder_path) REFERENCES drift_shadows (report_id, builder_path) ON DELETE CASCADE
);

CREATE TABLE drift_symbols (
    report_id  bigint NOT NULL REFERENCES drift_reports (id) ON DELETE CASCADE,
    symbol     text   NOT NULL,
    kind       text   NOT NULL CHECK (kind IN ('dead', 'known_dead', 'stray', 'firmware_retired', 'stale_entry')),
    reason     text,
    allowed    text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (report_id, symbol, kind)
);

CREATE TABLE drift_symbol_devices (
    report_id  bigint NOT NULL,
    symbol     text   NOT NULL,
    kind       text   NOT NULL,
    device     text   NOT NULL,
    PRIMARY KEY (report_id, symbol, kind, device),
    FOREIGN KEY (report_id, symbol, kind) REFERENCES drift_symbols (report_id, symbol, kind) ON DELETE CASCADE
);

CREATE TABLE drift_notices (
    report_id  bigint NOT NULL REFERENCES drift_reports (id) ON DELETE CASCADE,
    position   int    NOT NULL,
    text       text   NOT NULL,
    PRIMARY KEY (report_id, position)
);
