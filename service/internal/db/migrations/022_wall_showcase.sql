-- What the home page's mosaic may show (internal/snapshots, Showcase).
--
-- The mosaic is the first thing a visitor to openipc.org sees, and the wall
-- publishes whatever a camera sends. Two kinds of frame do not belong there:
--
-- A frame with nothing in it. A lens cap, a sensor stuck white, an IR scene
-- with no light: 7.5% of the frames on the wall on 2026-10-03 were flat grey,
-- white, black or blown out. Each frame's brightness percentiles over a 64x36
-- greyscale copy are measured once, from the decode the frame is checked
-- with, and the mosaic leaves out a camera whose newest frame is flat. The
-- rule is applied when the mosaic is read, so moving a threshold needs no
-- recomputation.
--
-- A camera nobody knows yet. Anyone can make a camera upload anything, and a
-- new one could put a picture of its choosing on the front page within
-- fifteen minutes. A camera appears there only once it has been uploading for
-- thirty days, which needs the day it was first seen -- and snapshots are
-- purged after two. cameras keeps that day per camera, written by the
-- trigger below on every upload and never purged: one row per camera, a few
-- thousand rows at most.
ALTER TABLE snapshots ADD COLUMN luma_p5 smallint;
ALTER TABLE snapshots ADD COLUMN luma_p50 smallint;
ALTER TABLE snapshots ADD COLUMN luma_p95 smallint;

CREATE TABLE cameras (
    mac_key    text PRIMARY KEY,
    first_seen timestamptz NOT NULL
);

CREATE FUNCTION cameras_seen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO cameras (mac_key, first_seen) VALUES (NEW.mac_key, NEW.created_at)
    ON CONFLICT (mac_key) DO NOTHING;
    RETURN NEW;
END $$;

CREATE TRIGGER cameras_seen AFTER INSERT ON snapshots
    FOR EACH ROW EXECUTE FUNCTION cameras_seen();

-- Every camera on the wall at the moment this runs. Earlier sightings, from
-- before the two days snapshots keep, can only lower first_seen:
--   INSERT ... ON CONFLICT (mac_key) DO UPDATE
--     SET first_seen = LEAST(cameras.first_seen, EXCLUDED.first_seen)
INSERT INTO cameras (mac_key, first_seen)
    SELECT mac_key, min(created_at) FROM snapshots GROUP BY mac_key
    ON CONFLICT (mac_key) DO NOTHING;
