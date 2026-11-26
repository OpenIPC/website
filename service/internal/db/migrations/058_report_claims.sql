-- A report sent without a Club member -- by ipctool, an AI agent or the
-- send form signed out -- is answered with a claim code, claim-XXXX-XXXX-XXXX,
-- that only its sender sees (#620). A member who enters it at /club makes
-- the report theirs, as if it had been sent signed in: listed on /club,
-- editable, and earning stars -- at once for a report already published,
-- else when a maintainer accepts it.
--
-- Only the code's sha256 is kept: the table holds nothing that claims a
-- report. A code is used once (claimed_at); the report it claimed is then
-- its member's in report_submissions, which is where who claimed it is.
CREATE TABLE report_claims (
    report_id   text PRIMARY KEY REFERENCES reports ON DELETE RESTRICT,
    code_sha256 text NOT NULL UNIQUE CHECK (code_sha256 ~ '^[0-9a-f]{64}$'),
    claimed_at  timestamptz
);

-- A claim gives a report sent with a board but no member (a row with
-- member_id NULL) its member: with openipc.report_claim = 'on' for its
-- transaction, the guard lets an UPDATE through that sets member_id alone,
-- from NULL, to a member that exists. Everything else it refuses as before.
CREATE OR REPLACE FUNCTION report_submissions_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce(current_setting('openipc.reports_guard', true), '') = 'off' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.report_id = OLD.report_id AND NEW.model_id IS NOT DISTINCT FROM OLD.model_id
       AND NEW.member_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND member_merge_move(to_jsonb(OLD), to_jsonb(NEW)) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND coalesce(current_setting('openipc.report_claim', true), '') = 'on'
       AND OLD.member_id IS NULL AND NEW.member_id IS NOT NULL
       AND to_jsonb(OLD) - 'member_id' = to_jsonb(NEW) - 'member_id'
       AND EXISTS (SELECT 1 FROM club_members WHERE id = NEW.member_id) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'owner reports are never changed or deleted (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
