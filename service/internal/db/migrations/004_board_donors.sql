-- Board catalogue, second source and beyond (cctvsp.ru, Xiongmai's own
-- catalogue): what a board's maker or seller says about a model, in several
-- languages, and the rule that one PCB design is one model however many
-- sources describe it.
--
-- A model now carries, per source: its name and description per locale
-- (board_model_texts), its specification table per locale
-- (board_model_specs), tags such as openipc-ready or discontinued
-- (board_model_tags) and links -- the stock firmware in OpenIPC/xmupdates,
-- the page it came from, the model it replaced or was replaced by
-- (board_links). A source's photos and files stay units, as before.
--
-- Deduplication is a constraint, not a habit: every code any source prints
-- for a model is a row in board_model_aliases, unique per maker, and an
-- import resolves a code through it before it creates anything. The code is
-- normalised as the importer and tools/board-donors/dedup.py do: upper case,
-- runs of space, underscore, slash and dot made one hyphen.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'cctvsp';
ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'xiongmai';
-- Stock firmware a seller hosts itself, next to the board it came on.
ALTER TYPE board_artifact_kind ADD VALUE IF NOT EXISTS 'firmware';

CREATE TABLE board_sources (
    id        text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    name      text NOT NULL,
    url       text NOT NULL,
    -- What the catalogue says about where this came from and on what terms.
    note      text NOT NULL,
    -- The pinned commit or snapshot the rows were imported from.
    ref       text NOT NULL,
    position  integer NOT NULL DEFAULT 0
);
INSERT INTO board_sources (id, name, url, note, ref, position) VALUES
    ('openhisiipcam', 'OpenHisiIpCam', 'https://github.com/OpenHisiIpCam',
     'The hardware catalogue of the OpenHisiIpCam project (2018), republished with its author''s permission.',
     '7e00f43bb3cde11a34eb8f75c83f70be9b2ae072', 1);

ALTER TABLE board_models ADD COLUMN category text;

CREATE TABLE board_model_aliases (
    maker_id   text NOT NULL REFERENCES board_manufacturers (id),
    code_norm  text NOT NULL CHECK (code_norm ~ '^[A-Z0-9][A-Z0-9-]*$'),
    model_id   text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    source     text NOT NULL,
    code_as_printed text NOT NULL,
    PRIMARY KEY (maker_id, code_norm)
);
CREATE INDEX board_model_aliases_by_model ON board_model_aliases (model_id);

-- Every model already in the catalogue answers to the code it was imported
-- under. Unidentified boards have no code and no alias.
INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed)
SELECT manufacturer_id,
       trim(both '-' from regexp_replace(regexp_replace(upper(model), '[\s_/.]+', '-', 'g'), '-{2,}', '-', 'g')),
       id, 'openhisiipcam', model
FROM board_models
WHERE model IS NOT NULL
ON CONFLICT DO NOTHING;

CREATE TABLE board_model_texts (
    model_id  text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    source    text NOT NULL REFERENCES board_sources (id),
    locale    text NOT NULL CHECK (locale IN ('en', 'ru', 'zh')),
    field     text NOT NULL CHECK (field IN ('name', 'description', 'features')),
    text      text NOT NULL,
    -- NULL for the source's own words; the locale it was translated from
    -- otherwise.
    translated_from text CHECK (translated_from IN ('en', 'ru', 'zh')),
    PRIMARY KEY (model_id, source, locale, field)
);

CREATE TABLE board_model_specs (
    model_id  text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    source    text NOT NULL REFERENCES board_sources (id),
    locale    text NOT NULL CHECK (locale IN ('en', 'ru', 'zh')),
    position  integer NOT NULL,
    label     text NOT NULL,
    value     text NOT NULL,
    PRIMARY KEY (model_id, source, locale, position)
);

CREATE TABLE board_model_tags (
    model_id  text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    source    text NOT NULL REFERENCES board_sources (id),
    tag       text NOT NULL CHECK (tag ~ '^[a-z0-9][a-z0-9:-]{0,63}$'),
    PRIMARY KEY (model_id, source, tag)
);
CREATE INDEX board_model_tags_by_tag ON board_model_tags (tag);

CREATE TABLE board_links (
    model_id  text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    source    text NOT NULL REFERENCES board_sources (id),
    position  integer NOT NULL,
    kind      text NOT NULL CHECK (kind IN ('stock_firmware', 'source_page', 'vendor_page', 'successor', 'predecessor', 'related')),
    label     text NOT NULL,
    url       text,
    -- For successor, predecessor and related: the other model, when the
    -- catalogue has it. Resolved through the aliases at import.
    target_model_id text REFERENCES board_models (id) ON DELETE SET NULL,
    PRIMARY KEY (model_id, source, position)
);
