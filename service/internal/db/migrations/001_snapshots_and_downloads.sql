-- The two tables openipc.org keeps, designed fresh (#293). Nothing here is
-- imported from the Rails MySQL database: the wall refills from live cameras
-- within one upload cycle, and download stats count from the day Go serves them.

CREATE TABLE snapshots (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- The address every public URL uses. Twenty hex characters and never all
    -- digits: nginx and the wall tell a retired numeric row id from a current
    -- id by whether it is a number, so an all-digit id would be answered 410
    -- for its whole two days.
    public_id             text NOT NULL UNIQUE
                          CHECK (public_id ~ '^[0-9a-f]{20}$' AND public_id !~ '^[0-9]+$'),

    mac_address           text NOT NULL,
    -- One camera, however it spells its MAC. MySQL's collation folded case
    -- and nothing folded "-" against ":"; this folds both, and every
    -- per-camera question (the interval, a camera's day, the latest frame per
    -- camera) is asked of this column.
    mac_key               text GENERATED ALWAYS AS (lower(translate(mac_address, ':-', ''))) STORED,
    -- HMAC-SHA256(secret_key_base, mac_key)[0,16], computed on insert. The
    -- public name of a camera; stored so that /open-wall/camera/<token> is
    -- one indexed lookup instead of hashing every camera on the wall.
    camera_token          text NOT NULL,

    ip_address            text,
    caption               text,
    firmware              text,
    flash_size            text,
    hostname              text,
    sensor                text,
    soc                   text,
    soc_temperature       text,
    streamer              text,
    uptime                text,

    -- What the upload was, which ActiveStorage used to keep in three tables.
    content_type          text   NOT NULL,
    byte_size             bigint NOT NULL,
    width                 integer,
    height                integer,

    -- Set once the four variants are on disk; until then the original is too,
    -- and the row is a work item (see internal/variants).
    variants_generated_at timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);

-- Latest frame per camera is DISTINCT ON (mac_key) over this, and a camera's
-- day is a range scan of it. The id tie-break is in the index because it is in
-- every ORDER BY: two frames can share a created_at.
CREATE INDEX snapshots_by_camera ON snapshots (mac_key, created_at DESC, id DESC);
CREATE INDEX snapshots_created_at ON snapshots (created_at);
CREATE INDEX snapshots_camera_token ON snapshots (camera_token);
CREATE INDEX snapshots_variants_pending ON snapshots (id) WHERE variants_generated_at IS NULL;

-- One row per firmware image a visitor started downloading. Stats, kept
-- indefinitely; the firmware itself is never kept beyond its current version.
CREATE TABLE downloads (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    soc_model   text    NOT NULL,
    flash_type  text    NOT NULL,
    release     text    NOT NULL,
    flash_size  integer,
    bytes       bigint,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX downloads_by_soc ON downloads (soc_model, created_at);
CREATE INDEX downloads_created_at ON downloads (created_at);
