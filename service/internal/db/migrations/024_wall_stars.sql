-- Stars for keeping a camera on the Open Wall (internal/wallstars).
--
-- A club member links a camera by making it upload a one-time code: pasted
-- into the WebUI's OpenWall caption on the firmware already in the field, or
-- sent as the upload's optional `club` field by firmware that has one. Only
-- someone who can configure the camera can make it send the code, which is
-- what a MAC alone could never prove. The code is cut out of the caption
-- before the row is stored, so it is never shown.
--
-- Stars are not written by the upload. A nightly settlement reads what the
-- cameras did -- on how many days they sent a picture worth showing -- and
-- writes the difference between what a camera has earned and what its ledger
-- holds, as reports.Decide does for a review.

-- The code an upload carried (the caption's or the `club` field's), until
-- the frame is published and the code is tried. Purged with the frame.
ALTER TABLE snapshots ADD COLUMN club_code text;
-- A 64-bit difference hash of the frame's 64x36 greyscale copy, taken from
-- the decode that measures its brightness (keyframe.Luma.Hash).
ALTER TABLE snapshots ADD COLUMN frame_hash bigint;

-- One row per camera per UTC day with a published frame, never purged:
-- snapshots go after two days, and the settlement counts months.
--   lit: a frame that day had something in it (the home page's brightness
--        rules, snapshots.Lit).
--   varied: a frame that day differed from the day's first by a few bits of
--        its hash -- a camera looping one stock picture never does.
CREATE TABLE camera_days (
    mac_key    text NOT NULL,
    day        date NOT NULL,
    frames     integer NOT NULL,
    lit        boolean NOT NULL,
    varied     boolean NOT NULL,
    first_hash bigint NOT NULL,
    PRIMARY KEY (mac_key, day)
);

-- A code a member asked for on /club: one camera, 24 hours.
CREATE TABLE club_camera_codes (
    code       text PRIMARY KEY,
    member_id  text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    mac_key    text
);
CREATE INDEX club_camera_codes_by_member ON club_camera_codes (member_id, created_at);

-- Which member a camera is linked to. A newer code from whoever controls the
-- camera moves it; unlinking deletes the row. What the camera already earned
-- stays with whoever it was paid to.
--   name: the camera's caption when it was linked, for when its frames
--        have all been purged (a camera silent for two days).
--   show_owner: the member's name on the camera's wall page (opt-in).
--   milestone, silent_day: which notices the settlement has already sent.
CREATE TABLE camera_links (
    mac_key    text PRIMARY KEY REFERENCES cameras ON DELETE CASCADE,
    member_id  text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    linked_at  timestamptz NOT NULL DEFAULT now(),
    name       text,
    show_owner boolean NOT NULL DEFAULT false,
    milestone  integer NOT NULL DEFAULT 0,
    silent_day date
);
CREATE INDEX camera_links_by_member ON camera_links (member_id, linked_at);

-- A camera found to be faked: it earns nothing more, and what it earned is
-- taken back (`openipc club wall-revoke`).
CREATE TABLE wall_revoked (
    mac_key text PRIMARY KEY,
    at      timestamptz NOT NULL DEFAULT now(),
    reason  text NOT NULL
);

-- The Open Wall's stars ledger, beside report_stars: inserted, never
-- changed, a member's stars the sum of their rows in both. An award is paid
-- once per camera and reason, ever -- re-linking a camera pays nothing again.
--   reason: join (the camera met the home page's bar), month:<n> (each
--   further 30 qualifying days), rare (the first such camera with its chip or
--   sensor); a revoke row repeats the reason of the award it takes back.
CREATE TABLE wall_stars (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    mac_key   text NOT NULL,
    kind      text NOT NULL CHECK (kind IN ('award', 'revoke')),
    reason    text NOT NULL,
    points    int NOT NULL,
    at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (mac_key, kind, reason)
);
CREATE INDEX wall_stars_by_member ON wall_stars (member_id, at);

CREATE FUNCTION wall_stars_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM club_members WHERE id = OLD.member_id) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'the stars ledger is never changed (% on %); see service/internal/wallstars', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER wall_stars_guard BEFORE UPDATE OR DELETE ON wall_stars
    FOR EACH ROW EXECUTE FUNCTION wall_stars_guard();
CREATE FUNCTION wall_stars_no_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'the stars ledger is never truncated; see service/internal/wallstars'
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER wall_stars_no_truncate BEFORE TRUNCATE ON wall_stars
    FOR EACH STATEMENT EXECUTE FUNCTION wall_stars_no_truncate();

-- The public leaderboard lists only members who asked to be on it.
ALTER TABLE club_members ADD COLUMN listed boolean NOT NULL DEFAULT false;
