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
-- fifteen minutes. A camera appears there only once it has been uploading
-- for a month, on many separate days -- one upload and a month of silence is
-- not a camera anyone has watched. That needs a camera's history, and
-- snapshots are purged after two days, so cameras keeps it: the first frame
-- the wall accepted, and on how many UTC days it has accepted one. Written
-- by snapshots.Store.MarkGenerated, so an upload the wall refused counts for
-- nothing; never purged, one row per camera.
ALTER TABLE snapshots ADD COLUMN luma_p5 smallint;
ALTER TABLE snapshots ADD COLUMN luma_p50 smallint;
ALTER TABLE snapshots ADD COLUMN luma_p95 smallint;

CREATE TABLE cameras (
    mac_key    text PRIMARY KEY,
    first_seen timestamptz NOT NULL,
    last_day   date NOT NULL,
    days       integer NOT NULL
);

-- The frames on the wall at the moment this runs: those with a picture (a
-- refused upload has no dimensions).
INSERT INTO cameras (mac_key, first_seen, last_day, days)
    SELECT mac_key, min(created_at), max((created_at AT TIME ZONE 'UTC')::date),
           count(DISTINCT (created_at AT TIME ZONE 'UTC')::date)
    FROM snapshots WHERE width IS NOT NULL GROUP BY mac_key;
