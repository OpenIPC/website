-- The club after its first review (PR #378).
--
-- Stars are a net, not a one-off: a report published, rejected and
-- published again earns its stars back, so the ledger may hold several
-- award and revoke rows for one part of a report. What a part has earned
-- is the sum of its rows; the review writes the difference.
ALTER TABLE report_stars DROP CONSTRAINT report_stars_report_id_position_kind_key;
CREATE INDEX report_stars_by_part ON report_stars (report_id, position);

-- A Telegram sign-in is finished only when the person who tapped Start
-- confirms it in the chat: a start link sent to someone else by whoever
-- asked for it would otherwise sign that browser in as them. claimed_by is
-- who tapped Start, waiting for Yes; requested_from is the asking address,
-- shown in the question so a sign-in nobody asked for is recognisable.
ALTER TABLE club_logins ADD COLUMN claimed_by text REFERENCES club_members ON DELETE CASCADE;
ALTER TABLE club_logins ADD COLUMN requested_from text NOT NULL DEFAULT '';
