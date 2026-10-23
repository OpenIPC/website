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
