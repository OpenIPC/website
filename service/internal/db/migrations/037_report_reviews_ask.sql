-- A reviewer may ask the sender instead of deciding: a review row 'ask',
-- whose note is the question. The report is "asked" then -- out of the
-- pending queue, off nothing it was not already off, its stars untouched --
-- until its sender edits it (a row 'edit', pending again) or a maintainer
-- decides anyway.
--
-- Additive: an image from before this one reads the newest row's decision
-- and finds a word it does not know. Its review queue (List) files that
-- under pending, and Store.status answers an empty state, which nothing
-- treats as published.
ALTER TABLE report_reviews DROP CONSTRAINT report_reviews_decision_check;
ALTER TABLE report_reviews ADD CONSTRAINT report_reviews_decision_check
    CHECK (decision IN ('publish', 'reject', 'withdraw', 'edit', 'ask'));

-- An answer from a sender who cannot edit: a report sent signed out cannot
-- be changed, so its sender answers with a new report from its receipt --
-- photos, a log, a note -- that names the one asked about. The new report
-- needs no board of its own; the queue shows it as that one's answer. Owner
-- reports' rows: written once, by the reports package, never changed.
CREATE TABLE report_answers (
    report_id  text PRIMARY KEY REFERENCES reports ON DELETE RESTRICT,
    answers    text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    CHECK (report_id <> answers)
);
CREATE INDEX report_answers_by_answered ON report_answers (answers);
CREATE TRIGGER report_answers_guard BEFORE UPDATE OR DELETE ON report_answers
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_answers_no_truncate BEFORE TRUNCATE ON report_answers
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
