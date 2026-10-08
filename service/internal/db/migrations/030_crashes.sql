-- Kernel crashes cameras recovered from (internal/crashes): the bundle the
-- firmware's S98crashlog makes of pstore's records, sent by the WebUI
-- (POST /api/v1/crashes) or by a member from /club.
--
-- A crash is filed under its signature, the top of its backtrace without
-- offsets, so one bug sent by many cameras is one signature. The events and
-- the bundles they came in are append-only, like owner reports; only a
-- takedown (openipc.crashes_guard = 'off', its own transaction) removes one.
-- Signatures are the maintainers' to triage, so they change.
--
-- Stars are paid by the nightly settlement (`openipc crashes settle`), never
-- by an upload, into crash_stars -- the third ledger beside report_stars and
-- wall_stars.

CREATE TABLE crash_signatures (
    id          text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{12}$'),
    class       text NOT NULL CHECK (class IN ('fatal', 'warning')),
    -- the worst kind any event under it was
    kind        text NOT NULL CHECK (kind IN ('bootloop', 'panic', 'oops', 'bug', 'warning')),
    title       text NOT NULL,
    frames      jsonb NOT NULL,
    first_seen  timestamptz NOT NULL DEFAULT now(),
    -- triage: open until a maintainer looks; bogus is a faked crash, which
    -- pays nothing and is taken back
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'confirmed', 'fixed', 'wontfix', 'bogus')),
    fixed_in    text NOT NULL DEFAULT '' CHECK (length(fixed_in) <= 200),
    issue_url   text NOT NULL DEFAULT '' CHECK (issue_url = '' OR issue_url ~ '^https://'),
    note        text NOT NULL DEFAULT '' CHECK (length(note) <= 4000),
    -- one bug found under two signatures: the events of this one count
    -- under that one
    merged_into text REFERENCES crash_signatures CHECK (merged_into <> id),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Who triaged what, as it happened.
CREATE TABLE crash_triage (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signature_id text NOT NULL REFERENCES crash_signatures,
    by_member    text NOT NULL,
    status       text NOT NULL,
    fixed_in     text NOT NULL,
    issue_url    text NOT NULL,
    merged_into  text,
    note         text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now()
);

-- A bundle as it was sent, never served: the log maintainers read is the
-- redacted copy in crash_events.
CREATE TABLE crash_bundles (
    sha256      text PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    bytes       bytea NOT NULL CHECK (length(bytes) <= 262144),
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crash_events (
    id             text PRIMARY KEY CHECK (id ~ '^c-[a-z0-9]{8}$'),
    -- the records' content, whatever tar or gzip carried them: the same
    -- crash sent twice from one camera is one event (UNIQUE below; two
    -- cameras' identical failsafe notes are two)
    content_sum    text NOT NULL CHECK (content_sum ~ '^[0-9a-f]{64}$'),
    signature_id   text NOT NULL REFERENCES crash_signatures,
    received_at    timestamptz NOT NULL DEFAULT now(),
    channel        text NOT NULL CHECK (channel IN ('webui', 'club', 'api')),
    -- the camera: its MAC as the wall keys it ('' when the sender did not
    -- say), and the member who sent it from /club
    mac_key        text NOT NULL DEFAULT '',
    member_id      text REFERENCES club_members ON DELETE SET NULL,
    client_key     text NOT NULL,
    kind           text NOT NULL,
    in_irq         boolean NOT NULL,
    self_inflicted boolean NOT NULL,
    title          text NOT NULL,
    -- what the camera said it runs, and what the log says it ran
    firmware       text NOT NULL DEFAULT '' CHECK (length(firmware) <= 200),
    majestic       text NOT NULL DEFAULT '' CHECK (length(majestic) <= 200),
    soc            text NOT NULL DEFAULT '' CHECK (length(soc) <= 100),
    sensor         text NOT NULL DEFAULT '' CHECK (length(sensor) <= 100),
    board          text NOT NULL DEFAULT '',
    machine        text NOT NULL DEFAULT '',
    kernel         text NOT NULL DEFAULT '',
    kernel_build   text NOT NULL DEFAULT '',
    kernel_built   timestamptz,
    cmdline        text NOT NULL DEFAULT '',
    uptime         double precision,
    records        int NOT NULL,
    modules        text[] NOT NULL DEFAULT '{}',
    fatal          jsonb NOT NULL,
    before         jsonb NOT NULL DEFAULT '[]',
    anomalies      jsonb NOT NULL DEFAULT '{}',
    leadup         jsonb NOT NULL DEFAULT '[]',
    -- the firmware's description of the camera (meta.json), redacted
    meta           jsonb,
    -- every record, the identifiers replaced by keyed hashes
    redacted       text NOT NULL,
    bundle_sha256  text NOT NULL REFERENCES crash_bundles ON DELETE RESTRICT,
    UNIQUE (content_sum, mac_key)
);
CREATE INDEX crash_events_by_signature ON crash_events (signature_id, received_at);
CREATE INDEX crash_events_by_member ON crash_events (member_id, received_at) WHERE member_id IS NOT NULL;
CREATE INDEX crash_events_by_camera ON crash_events (mac_key, received_at) WHERE mac_key <> '';
CREATE INDEX crash_events_by_client ON crash_events (client_key, received_at);

CREATE FUNCTION crashes_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce(current_setting('openipc.crashes_guard', true), '') = 'off' THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    -- a member who leaves: their events stay, no longer theirs
    IF TG_OP = 'UPDATE' AND TG_TABLE_NAME = 'crash_events' THEN
        IF NEW.member_id IS NULL AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id)
           AND to_jsonb(NEW) - 'member_id' = to_jsonb(OLD) - 'member_id' THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'crashes are never changed or deleted (% on %); see service/internal/crashes', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER crash_events_guard BEFORE UPDATE OR DELETE ON crash_events
    FOR EACH ROW EXECUTE FUNCTION crashes_guard();
CREATE TRIGGER crash_bundles_guard BEFORE UPDATE OR DELETE ON crash_bundles
    FOR EACH ROW EXECUTE FUNCTION crashes_guard();
CREATE TRIGGER crash_events_no_truncate BEFORE TRUNCATE ON crash_events
    FOR EACH STATEMENT EXECUTE FUNCTION crashes_guard();
CREATE TRIGGER crash_bundles_no_truncate BEFORE TRUNCATE ON crash_bundles
    FOR EACH STATEMENT EXECUTE FUNCTION crashes_guard();

-- The crashes' stars ledger, beside report_stars and wall_stars: inserted,
-- never changed. What a member holds for a bug is the net of their rows
-- under it -- its signature and every signature merged into it -- so a
-- revoke (a signature found bogus) and a later award (the decision undone)
-- are both just rows, and the settlement pays only what is not held.
--   reason: report (a crash of the bug from the member's linked camera, or
--   sent by them from /club without a camera: mac_key ''), first (they
--   reported the bug first and a maintainer confirmed it), fixed (it was
--   fixed); a revoke row repeats the reason of the award it takes back.
CREATE TABLE crash_stars (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id    text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    signature_id text NOT NULL REFERENCES crash_signatures,
    mac_key      text NOT NULL DEFAULT '',
    kind         text NOT NULL CHECK (kind IN ('award', 'revoke')),
    reason       text NOT NULL CHECK (reason IN ('report', 'first', 'fixed')),
    points       int NOT NULL,
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crash_stars_by_member ON crash_stars (member_id, at);
CREATE INDEX crash_stars_by_signature ON crash_stars (signature_id);

CREATE FUNCTION crash_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/crashes', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER crash_stars_guard BEFORE UPDATE OR DELETE ON crash_stars
    FOR EACH ROW EXECUTE FUNCTION crash_stars_guard();
CREATE FUNCTION crash_stars_no_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'the stars ledger is never truncated; see service/internal/crashes'
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER crash_stars_no_truncate BEFORE TRUNCATE ON crash_stars
    FOR EACH STATEMENT EXECUTE FUNCTION crash_stars_no_truncate();
