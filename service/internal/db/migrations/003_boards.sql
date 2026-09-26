-- The board catalogue: camera PCBs as people found them, with the evidence
-- each one left -- photos, UART pinouts, factory flash dumps, U-Boot console
-- captures, boot logs (firmware#659).
--
-- Four levels, because that is how the evidence arrives:
--
--   manufacturer  one maker, however its boards are marked (XM is Xiongmai)
--   model         one PCB design; the SoC is soldered on, so it lives here
--   unit          one physical board somebody held; the sensor module and
--                 flash chip vary between units of the same model
--   artifact      one file about that unit, of one kind
--
-- So "which models have photos but no pinout" and "which U-Boot consoles
-- mention mtdparts" are plain queries. The first rows come from the
-- OpenHisiIpCam project's archive (`openipc boards import-openhisiipcam`);
-- `contributor` is for boards people send later.
--
-- The files themselves are under BOARDS_ROOT, served by nginx at
-- /board-files/<path>. Text kinds keep their full text in `content` as well:
-- search reads the database, never the disk. The text is small (hundreds of
-- lines per board), so search scans it line by line and needs no index.

CREATE TYPE board_source        AS ENUM ('openhisiipcam', 'contributor');
CREATE TYPE board_artifact_kind AS ENUM (
    'photo_front', 'photo_back', 'photo_other', 'pinout',
    'flash_dump', 'uboot_env', 'boot_log', 'document', 'note');

CREATE TABLE board_manufacturers (
    id        text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    name      text NOT NULL,
    -- How boards are marked, when it is not the name: silkscreen, stickers.
    aliases   text[] NOT NULL DEFAULT '{}',
    website   text,
    position  integer NOT NULL DEFAULT 0
);

CREATE TABLE board_models (
    id               text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
    manufacturer_id  text NOT NULL REFERENCES board_manufacturers (id),
    -- The PCB marking, e.g. BLK16CV-S1-38X38. NULL when nobody could read it.
    model            text,
    -- The catalogue's SoC slug (data/catalogue), when the SoC is one OpenIPC
    -- knows; soc_label is what the source printed, typos and all.
    soc              text CHECK (soc ~ '^[a-z0-9][a-z0-9._-]*$'),
    soc_label        text,
    -- The SDK family the source filed the board under, e.g. hi3516cv200.
    family           text,
    notes            text,
    position         integer NOT NULL DEFAULT 0
);
CREATE INDEX board_models_by_manufacturer ON board_models (manufacturer_id, position);
CREATE INDEX board_models_by_soc ON board_models (soc);

CREATE TABLE board_units (
    id              text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,159}$'),
    model_id        text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    sensor          text,
    flash_chip      text,
    flash_size_mb   integer CHECK (flash_size_mb > 0),
    source          board_source NOT NULL,
    -- Where the unit came from, precisely enough to find it again: the
    -- pinned commit and heading, or a pull request. An import skips a unit
    -- whose source_ref it already holds.
    source_ref      text NOT NULL UNIQUE,
    contributed_by  text,
    notes           text,
    position        integer NOT NULL DEFAULT 0,
    ingested_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX board_units_by_model ON board_units (model_id, position);

CREATE TABLE board_artifacts (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    unit_id     text NOT NULL REFERENCES board_units (id) ON DELETE CASCADE,
    kind        board_artifact_kind NOT NULL,
    -- The file's name as the source had it.
    name        text NOT NULL,
    -- Relative to BOARDS_ROOT; the URL is /board-files/<path>.
    path        text NOT NULL UNIQUE CHECK (path ~ '^[a-z0-9][a-z0-9-]*/[A-Za-z0-9._-]+$'),
    thumb_path  text CHECK (thumb_path ~ '^[a-z0-9][a-z0-9-]*/[A-Za-z0-9._-]+$'),
    mime        text NOT NULL,
    bytes       bigint NOT NULL CHECK (bytes >= 0),
    sha256      text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    width       integer,
    height      integer,
    -- The whole text of uboot_env, boot_log and note; NULL for the rest.
    content     text,
    position    integer NOT NULL DEFAULT 0
);
CREATE INDEX board_artifacts_by_unit ON board_artifacts (unit_id, kind, position);

-- Every name=value a U-Boot console printed, so "units whose bootargs set
-- mtdparts" or "which units carry an ethaddr" are exact questions.
CREATE TABLE board_uboot_vars (
    artifact_id  bigint NOT NULL REFERENCES board_artifacts (id) ON DELETE CASCADE,
    key          text NOT NULL,
    value        text NOT NULL,
    PRIMARY KEY (artifact_id, key)
);
CREATE INDEX board_uboot_vars_by_key ON board_uboot_vars (key);

-- What each model has, counted: the catalogue's "no pinout yet" is a zero.
CREATE VIEW board_model_coverage AS
SELECT m.id AS model_id,
       count(DISTINCT u.id)                                                        AS units,
       count(a.id) FILTER (WHERE a.kind IN ('photo_front', 'photo_back', 'photo_other')) AS photos,
       count(a.id) FILTER (WHERE a.kind = 'pinout')                                AS pinouts,
       count(a.id) FILTER (WHERE a.kind = 'flash_dump')                            AS flash_dumps,
       count(a.id) FILTER (WHERE a.kind = 'uboot_env')                             AS uboot_envs,
       count(a.id) FILTER (WHERE a.kind = 'boot_log')                              AS boot_logs,
       count(a.id) FILTER (WHERE a.kind = 'document')                              AS documents
FROM board_models m
LEFT JOIN board_units u ON u.model_id = m.id
LEFT JOIN board_artifacts a ON a.unit_id = u.id
GROUP BY m.id;
