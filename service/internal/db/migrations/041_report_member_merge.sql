-- One person's two Club accounts made one (`openipc club merge`, #519).
--
-- Signing in another way while signed out makes a new member, and an
-- identity never moves once it is a member's, so one person can end up with
-- two accounts: their stars on one, their maintainer's GitHub on the other.
-- A merge moves everything of one member to the other and deletes the
-- emptied account.
--
-- The ledgers and the reports cannot be moved by writing a revoke on one
-- member and an award on the other: an award on the wall is paid once per
-- camera and reason (wall_stars' UNIQUE), and a revoke there means the
-- camera was faked. So a merge rewrites who the rows belong to, and only
-- that: with openipc.member_merge = 'on' for its transaction, the guards let
-- an UPDATE through that changes member_id alone, to a member that exists.
-- Everything else they refuse as before. What was moved, and from whom, is
-- in club_merges (migration 042).

CREATE FUNCTION member_merge_move(o jsonb, n jsonb) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT coalesce(current_setting('openipc.member_merge', true), '') = 'on'
       AND n->>'member_id' IS NOT NULL
       AND o - 'member_id' = n - 'member_id'
       AND EXISTS (SELECT 1 FROM club_members WHERE id = n->>'member_id')
$$;

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
    RAISE EXCEPTION 'owner reports are never changed or deleted (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;

CREATE OR REPLACE FUNCTION report_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    IF coalesce(current_setting('openipc.reports_guard', true), '') = 'off' THEN
        RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    END IF;
    IF TG_OP = 'UPDATE' AND member_merge_move(to_jsonb(OLD), to_jsonb(NEW)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/reports', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;

