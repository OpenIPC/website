-- The Open Wall's stars, after review (internal/wallstars).
--
-- A code that reaches a camera already linked to another member links
-- nothing: anyone can upload as any MAC, so moving a link on a code alone
-- would let whoever learns a camera's MAC take it and the stars it earns.
-- The code is marked blocked instead, so /club can say why nothing happened;
-- the owner unlinks it, or a maintainer does (`openipc club wall-unlink`).
ALTER TABLE club_camera_codes ADD COLUMN blocked_at timestamptz;

-- What the settlement has to tell a member, kept until the bot has said it.
-- A notice the bot could not deliver is tried again by the next run, for a
-- week; one for a member without Telegram, or who muted the bot, is done.
CREATE TABLE wall_notices (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id  text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('stars', 'milestone', 'silent')),
    camera     text NOT NULL,
    token      text NOT NULL,
    points     int NOT NULL DEFAULT 0,
    joined     boolean NOT NULL DEFAULT false,
    rare       boolean NOT NULL DEFAULT false,
    days       int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at    timestamptz
);
CREATE INDEX wall_notices_pending ON wall_notices (created_at) WHERE sent_at IS NULL;

-- When a linked camera took one of its member's slots. Days count from here,
-- not from the link: a camera over the cap of three builds up nothing it can
-- cash in when a slot frees. Set at the link, cleared while the camera is
-- over the cap, and set again when it gets a slot (wallstars.Settle).
ALTER TABLE camera_links ADD COLUMN counted_since timestamptz;
UPDATE camera_links SET counted_since = linked_at;
