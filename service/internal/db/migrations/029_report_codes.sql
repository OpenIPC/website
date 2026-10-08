-- A Club member's code for a report sent from outside the site: ipctool on
-- the camera (`ipctool upload --note club-XXXX-XXXX`, which every ipctool in
-- the field can send) or an agent. The upload that carries a live code is
-- the member's -- listed on /club, earning stars once published -- and the
-- code is cut out of the note before anything is stored.
--
-- A code may join a report the member already sent: the photos of a camera,
-- then ipctool's report from the same camera. The new report takes the
-- board that one named, or the camera it proposed, so the review files both
-- under one board.
--
-- The codes are the club's to change (used once, then marked), not owner
-- reports: no guard. The reports they join and were used by are, and are
-- never deleted.
CREATE TABLE report_codes (
    code        text PRIMARY KEY CHECK (code ~ '^club-[23456789A-HJ-NP-Z]{4}-[23456789A-HJ-NP-Z]{4}$'),
    member_id   text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    joins       text REFERENCES reports ON DELETE RESTRICT,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    used_by     text UNIQUE REFERENCES reports ON DELETE RESTRICT
);
CREATE INDEX report_codes_by_member ON report_codes (member_id, created_at);
