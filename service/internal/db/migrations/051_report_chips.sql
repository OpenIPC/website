-- The chip a maintainer read from a report, where what it says is missing or
-- wrong: a programmer's read of the flash comes without ipctool's output, so
-- it names no chip, and r-5uwdebux's device trees say anyka,ak3918av130.
--
-- Append-only, as report_edits: the latest row for a report is its chip,
-- one with an empty model takes the correction back, and the history is
-- who said so and on what evidence. The report's own chip_vendor and
-- chip_model stay as sent; report_chip is what every reader of a chip
-- uses, and it changes only through reports.Store.SetChip.
CREATE TABLE report_chips (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id   text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    chip_vendor text NOT NULL DEFAULT '' CHECK (length(chip_vendor) <= 100),
    chip_model  text NOT NULL DEFAULT '' CHECK (length(chip_model) <= 100),
    -- what shows it: "device tree: anyka,ak3918av130". Required for a chip,
    -- and for taking one back.
    evidence    text NOT NULL CHECK (evidence <> '' AND length(evidence) <= 200),
    by_who      text NOT NULL CHECK (by_who <> '' AND length(by_who) <= 200),
    at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX report_chips_by_report ON report_chips (report_id, id);
CREATE TRIGGER report_chips_guard BEFORE UPDATE OR DELETE ON report_chips
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_chips_no_truncate BEFORE TRUNCATE ON report_chips
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();

-- Each report's chip: the latest correction's, else the one it was sent with.
CREATE VIEW report_chip AS
SELECT r.id AS report_id,
       CASE WHEN c.chip_model <> '' THEN c.chip_vendor ELSE r.chip_vendor END AS chip_vendor,
       CASE WHEN c.chip_model <> '' THEN c.chip_model ELSE r.chip_model END AS chip_model,
       coalesce(c.chip_model <> '', false) AS corrected
FROM reports r
LEFT JOIN LATERAL (
    SELECT chip_vendor, chip_model FROM report_chips
    WHERE report_id = r.id ORDER BY id DESC LIMIT 1
) c ON true;

-- Migration 040's rule, over the chips as corrected: an identifier is one
-- camera only while every report carrying it names one chip.
CREATE OR REPLACE FUNCTION report_identifier_one_chip(k text, v text) RETURNS boolean
    LANGUAGE sql STABLE AS $$
    SELECT count(DISTINCT chip) <= 1 FROM (
        SELECT lower(c.chip_vendor) || '/' || lower(c.chip_model) AS chip
        FROM reports r JOIN report_chip c ON c.report_id = r.id
        WHERE r.id_hashes @> jsonb_build_object(k, v)
        UNION ALL
        SELECT chip FROM report_star_cameras WHERE key = k || ':' || v
    ) carriers
$$;
