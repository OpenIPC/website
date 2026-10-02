-- Who sent a report, and for which board, when it came through the site's
-- send form (POST /api/v1/club/reports); and the stars it earned. Both are
-- owner reports' own rows (internal/reports), guarded like the rest:
-- inserted, never changed.
--
-- A report sent by a signed-in member is theirs: they see its state and
-- download its private backup, and nobody else but a maintainer can. The
-- board it names is the sender's word, not a review's link -- report_models
-- stays the reviewer's.
CREATE TABLE report_submissions (
    report_id  text PRIMARY KEY REFERENCES reports ON DELETE RESTRICT,
    member_id  text REFERENCES club_members ON DELETE SET NULL,
    model_id   text REFERENCES board_models ON DELETE RESTRICT ON UPDATE RESTRICT
);
CREATE INDEX report_submissions_by_member ON report_submissions (member_id);

-- The stars ledger. A row is written when a maintainer publishes a report
-- (an award per file, and one for ipctool's output) and its negative when
-- a published report is rejected or withdrawn afterwards; nothing is ever
-- edited, so a member's stars are the sum of their rows.
CREATE TABLE report_stars (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id  text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    report_id  text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    -- the file's position, 0 for the report's ipctool output
    position   int NOT NULL CHECK (position >= 0),
    points     int NOT NULL,
    -- award: what was accepted; revoke: the award taken back
    kind       text NOT NULL CHECK (kind IN ('award', 'revoke')),
    reason     text NOT NULL,
    at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (report_id, position, kind)
);
CREATE INDEX report_stars_by_member ON report_stars (member_id, at);

-- ON DELETE SET NULL and CASCADE above run as the system, which the guard
-- would refuse: an account's deletion detaches its reports (they stay, as
-- reports always do) and drops its ledger. The guard lets those through by
-- looking at what changed: only member_id, to NULL.
CREATE FUNCTION report_submissions_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce(current_setting('openipc.reports_guard', true), '') = 'off' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.report_id = OLD.report_id AND NEW.model_id IS NOT DISTINCT FROM OLD.model_id
       AND NEW.member_id IS NULL THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'owner reports are never changed or deleted (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER report_submissions_guard BEFORE UPDATE OR DELETE ON report_submissions
    FOR EACH ROW EXECUTE FUNCTION report_submissions_guard();
CREATE TRIGGER report_submissions_no_truncate BEFORE TRUNCATE ON report_submissions
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();

-- The ledger refuses updates; a row goes only with its member's account.
CREATE FUNCTION report_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    IF coalesce(current_setting('openipc.reports_guard', true), '') = 'off' THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER report_stars_guard BEFORE UPDATE OR DELETE ON report_stars
    FOR EACH ROW EXECUTE FUNCTION report_stars_guard();
CREATE TRIGGER report_stars_no_truncate BEFORE TRUNCATE ON report_stars
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
