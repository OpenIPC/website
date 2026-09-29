-- Owner reports: what a camera's owner, or an AI coding agent working on a
-- bench, sends about a board -- ipctool's YAML, optionally a full-flash
-- backup, photos, a boot log, the U-Boot environment. Uploaded anonymously
-- (POST /api/v1/reports), stored at once, public only after review.
--
-- A report is the one thing in the catalogue nobody can re-create: the board
-- catalogue's other rows are rebuilt from pinned archives, a report exists
-- once, on this host. So it is kept apart from everything an import touches,
-- and a mistake elsewhere cannot reach it:
--
--   * Its own tables. No importer names them (service/deploytest fails if
--     one does), and nothing cascades into them: the link to a board model is
--     ON DELETE RESTRICT, so deleting a model that has a report fails instead
--     of taking the report with it (migration 008 deleted models by hand).
--   * Triggers refuse UPDATE, DELETE and TRUNCATE outright. The one way past
--     them is `openipc reports takedown|link`, which sets
--     openipc.reports_guard = 'off' for its own transaction only.
--   * Its files are content-addressed under REPORTS_ROOT, beside
--     BOARDS_ROOT and never inside it, written once and never overwritten.

CREATE TABLE reports (
    id             text PRIMARY KEY CHECK (id ~ '^r-[a-z0-9]{8}$'),
    received_at    timestamptz NOT NULL DEFAULT now(),
    channel        text NOT NULL CHECK (channel IN ('ipctool', 'agent', 'web')),
    tool           text NOT NULL DEFAULT '' CHECK (length(tool) <= 200),
    note           text NOT NULL DEFAULT '' CHECK (length(note) <= 4000),
    -- the YAML as sent, and the copy with the board's identifiers replaced
    -- by keyed hashes -- the only one ever served
    yaml           text NOT NULL,
    yaml_public    text NOT NULL,
    yaml_sha256    text NOT NULL CHECK (yaml_sha256 ~ '^[0-9a-f]{64}$'),
    -- what the YAML says, for matching and listing
    chip_vendor    text NOT NULL DEFAULT '',
    chip_model     text NOT NULL DEFAULT '',
    sensor         text NOT NULL DEFAULT '',
    flash_id       text NOT NULL DEFAULT '',
    flash_size     text NOT NULL DEFAULT '',
    board_vendor   text NOT NULL DEFAULT '',
    board_model    text NOT NULL DEFAULT '',
    main_app       text NOT NULL DEFAULT '',
    -- keyed hashes of MAC, die ID and cloud ID: two reports from one board
    -- match without either identifier being stored twice or served
    id_hashes      jsonb NOT NULL DEFAULT '{}',
    backup_consent text NOT NULL CHECK (backup_consent IN ('none', 'private', 'public')),
    client_hash    text NOT NULL
);
CREATE INDEX reports_by_client ON reports (client_hash, received_at);
CREATE INDEX reports_by_chip ON reports (lower(chip_model));

CREATE TABLE report_files (
    report_id     text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    position      int NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('backup', 'photo', 'boot_log', 'uboot_env', 'note', 'document')),
    name          text NOT NULL CHECK (name ~ '^[A-Za-z0-9._-]{1,120}$'),
    mime          text NOT NULL,
    sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    bytes         bigint NOT NULL CHECK (bytes > 0),
    -- the copy served once the report is published: the file itself, or for
    -- text a copy with the identifiers replaced. NULL: never served (a
    -- backup its owner did not agree to share).
    public_sha256 text CHECK (public_sha256 ~ '^[0-9a-f]{64}$'),
    public_bytes  bigint,
    PRIMARY KEY (report_id, position)
);
CREATE INDEX report_files_by_sha256 ON report_files (sha256);
CREATE INDEX report_files_by_public_sha256 ON report_files (public_sha256);

-- A report's state is its newest review; no review is "pending".
CREATE TABLE report_reviews (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    decision  text NOT NULL CHECK (decision IN ('publish', 'reject', 'withdraw')),
    by        text NOT NULL CHECK (by <> ''),
    at        timestamptz NOT NULL DEFAULT now(),
    note      text NOT NULL DEFAULT ''
);
CREATE INDEX report_reviews_by_report ON report_reviews (report_id, id DESC);

CREATE TABLE report_models (
    report_id text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    model_id  text NOT NULL REFERENCES board_models ON DELETE RESTRICT ON UPDATE RESTRICT,
    by        text NOT NULL CHECK (by <> ''),
    at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (report_id, model_id)
);
CREATE INDEX report_models_by_model ON report_models (model_id);

-- The key for id_hashes and client_hash, made once per database. It is
-- backed up with the rows it keys; nothing else needs it.
CREATE TABLE report_key (
    one boolean PRIMARY KEY DEFAULT true CHECK (one),
    key text NOT NULL
);
INSERT INTO report_key (key)
VALUES (replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', ''));

CREATE FUNCTION reports_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce(current_setting('openipc.reports_guard', true), '') <> 'off' THEN
        RAISE EXCEPTION 'owner reports are never changed or deleted (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER reports_guard BEFORE UPDATE OR DELETE ON reports
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_files_guard BEFORE UPDATE OR DELETE ON report_files
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_reviews_guard BEFORE UPDATE OR DELETE ON report_reviews
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_models_guard BEFORE UPDATE OR DELETE ON report_models
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_key_guard BEFORE UPDATE OR DELETE ON report_key
    FOR EACH ROW EXECUTE FUNCTION reports_guard();

CREATE TRIGGER reports_no_truncate BEFORE TRUNCATE ON reports
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_files_no_truncate BEFORE TRUNCATE ON report_files
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_reviews_no_truncate BEFORE TRUNCATE ON report_reviews
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_models_no_truncate BEFORE TRUNCATE ON report_models
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_key_no_truncate BEFORE TRUNCATE ON report_key
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
