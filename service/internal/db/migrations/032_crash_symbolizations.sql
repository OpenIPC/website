-- majestic's own crashes beside the kernel's (internal/crashes): the dump
-- majestic writes when it dies of a fatal signal (/etc/crash/majestic.dump),
-- sent the way the kernel's records are. They are class 'user', kind
-- 'signal', and only maintainers see them: a frame of majestic names a
-- function and a line of its source, which the public lists never show.
ALTER TABLE crash_signatures DROP CONSTRAINT crash_signatures_class_check;
ALTER TABLE crash_signatures ADD CONSTRAINT crash_signatures_class_check
    CHECK (class IN ('fatal', 'warning', 'user'));
ALTER TABLE crash_signatures DROP CONSTRAINT crash_signatures_kind_check;
ALTER TABLE crash_signatures ADD CONSTRAINT crash_signatures_kind_check
    CHECK (kind IN ('bootloop', 'panic', 'oops', 'bug', 'warning', 'signal'));

-- A dump as the camera wrote it, until it is symbolized. Its stack slice is
-- majestic's memory at the crash and can hold what it was handling -- a
-- request, the environment -- so it is never served, and it is deleted once
-- the backtrace is made (or given up on). The bundle kept in crash_bundles
-- for a majestic crash is the dump without its stack.
CREATE TABLE crash_dumps (
    event_id    text PRIMARY KEY REFERENCES crash_events ON DELETE CASCADE,
    bytes       bytea NOT NULL CHECK (length(bytes) <= 262144),
    received_at timestamptz NOT NULL DEFAULT now()
);

-- What the symbolizer (the firmware role) made of a dump: the backtrace from
-- the debuginfo majestic's CI publishes for the build-id, and the callers a
-- scan of the stack found past where it stopped. One row per event, updated
-- by the symbolizer only: pending while it retries (debuginfo not published
-- yet, a firmware build not fetched), then done or failed.
--
-- A symbolized crash is filed under the signature its backtrace makes;
-- provisional is the one it was filed under when it arrived, made of the
-- module and offset of the faulting instruction.
CREATE TABLE crash_symbolizations (
    event_id     text PRIMARY KEY REFERENCES crash_events ON DELETE CASCADE,
    status       text NOT NULL CHECK (status IN ('pending', 'done', 'failed')),
    attempts     int NOT NULL DEFAULT 0,
    next_try     timestamptz,
    provisional  text NOT NULL,
    -- [{fn, module, file, line, offset, probable}], innermost first
    frames       jsonb NOT NULL DEFAULT '[]',
    -- what was used: the majestic build-id, the firmware build, the
    -- libraries found
    sources      jsonb NOT NULL DEFAULT '{}',
    error        text NOT NULL DEFAULT '' CHECK (length(error) <= 2000),
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crash_symbolizations_pending ON crash_symbolizations (next_try) WHERE status = 'pending';
