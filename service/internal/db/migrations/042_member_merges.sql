-- Two Club accounts made one (#519), the rest of migration 041: the wall's
-- and the crashes' guards let a merge move a row's owner, and every merge is
-- on record.

CREATE OR REPLACE FUNCTION wall_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' AND member_merge_move(to_jsonb(OLD), to_jsonb(NEW)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/wallstars', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;

CREATE OR REPLACE FUNCTION crash_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' AND member_merge_move(to_jsonb(OLD), to_jsonb(NEW)) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/crashes', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;

CREATE OR REPLACE FUNCTION crashes_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
        -- a member merged into another: their events are that member's
        IF member_merge_move(to_jsonb(OLD), to_jsonb(NEW)) THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'crashes are never changed or deleted (% on %); see service/internal/crashes', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;

-- Every merge, append-only, and outliving both accounts: from_member is
-- gone once it is written, so neither is a foreign key. The history that
-- still names it -- who edited a report, who triaged a crash -- is read
-- through here.
--   moved: rows moved per table; stars: points moved per ledger
CREATE TABLE club_merges (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    from_member text NOT NULL,
    from_name   text NOT NULL,
    into_member text NOT NULL,
    moved       jsonb NOT NULL,
    stars       jsonb NOT NULL,
    reason      text NOT NULL CHECK (reason <> ''),
    at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX club_merges_by_from ON club_merges (from_member);

CREATE FUNCTION club_merges_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'a merge is never changed or deleted (% on %); see service/internal/club', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER club_merges_guard BEFORE UPDATE OR DELETE ON club_merges
    FOR EACH ROW EXECUTE FUNCTION club_merges_guard();
CREATE TRIGGER club_merges_no_truncate BEFORE TRUNCATE ON club_merges
    FOR EACH STATEMENT EXECUTE FUNCTION club_merges_guard();
