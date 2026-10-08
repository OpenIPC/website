-- A member may change what they sent -- the note, the camera they proposed,
-- a file removed or added -- through the reports package only (Store.Edit),
-- as Takedown does: the guard stands down for that one transaction. Every
-- change is recorded here, append-only, so the history is still the state.
--
-- An edit to a report a maintainer has already decided adds a review row of
-- its own, decision 'edit': the report is pending again, off the board and
-- the public pages, until a maintainer accepts it once more. An edit to a
-- pending report needs no such row.
ALTER TABLE report_reviews DROP CONSTRAINT report_reviews_decision_check;
ALTER TABLE report_reviews ADD CONSTRAINT report_reviews_decision_check
    CHECK (decision IN ('publish', 'reject', 'withdraw', 'edit'));

CREATE TABLE report_edits (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id  text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    member_id  text NOT NULL,
    at         timestamptz NOT NULL DEFAULT now(),
    -- what changed, for the reviewer: "note", "camera", "+2 photo", "-1 file"
    what       text NOT NULL CHECK (what <> '')
);
CREATE INDEX report_edits_by_report ON report_edits (report_id, at);
CREATE TRIGGER report_edits_guard BEFORE UPDATE OR DELETE ON report_edits
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_edits_no_truncate BEFORE TRUNCATE ON report_edits
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
