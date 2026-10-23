-- Stars per camera, not per report (reports/camera.go): what a member's
-- reports about one camera hold of ipctool's output and of the best dump is
-- paid once. A takedown blanks a report's identifiers and deletes its files
-- but keeps the stars it was paid (the owner's privacy, not a penalty), so
-- what made it the camera's and which of its parts were the camera's share
-- is written here when its stars are, and outlives the takedown: a later
-- report of the same camera is paid only what the withdrawn one was not.
--
-- Keyed hashes only, as reports.id_hashes holds them ("mac:<hash>"), never
-- an identifier in clear, with the chip the report named (vendor/model,
-- lower case): an identifier counts as a camera only while every report
-- carrying it names one chip (report_identifier_one_chip). Inserted, never
-- changed, guarded as the reports are.
CREATE TABLE report_star_cameras (
    report_id text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    key       text NOT NULL CHECK (key ~ '^(mac|die_id|cloud_id):[0-9a-f]+$'),
    chip      text NOT NULL,
    PRIMARY KEY (report_id, key)
);
CREATE INDEX report_star_cameras_by_key ON report_star_cameras (key);

-- The part of the camera's share a ledger position is: ipctool's output
-- (yaml) or a flash dump (dump).
CREATE TABLE report_star_parts (
    report_id text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    position  int NOT NULL CHECK (position >= 0),
    class     text NOT NULL CHECK (class IN ('yaml', 'dump')),
    PRIMARY KEY (report_id, position)
);

CREATE TRIGGER report_star_cameras_guard BEFORE UPDATE OR DELETE ON report_star_cameras
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_star_cameras_no_truncate BEFORE TRUNCATE ON report_star_cameras
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_star_parts_guard BEFORE UPDATE OR DELETE ON report_star_parts
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_star_parts_no_truncate BEFORE TRUNCATE ON report_star_parts
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();

-- When an identifier is one camera (#502), for the review queue's kin and
-- the stars alike:
--
--   * An Anyka's die_id is none: ipctool prints the chip family's ID
--     register under chip.id (0x2016...), the same in every Anyka of the
--     family. Reports sent before IDHashes left it out still carry it.
--   * Any identifier is one camera only while every report carrying it --
--     its keyed hash in id_hashes, or kept with a withdrawn report's stars
--     -- names one chip: a factory-default MAC is on every unit a maker
--     shipped.
CREATE FUNCTION report_anyka(vendor text, model text) RETURNS boolean
    LANGUAGE sql IMMUTABLE AS $$
    SELECT lower(coalesce(vendor, '')) = 'anyka' OR coalesce(model, '') ILIKE 'AK%'
$$;

CREATE FUNCTION report_identifier_one_chip(k text, v text) RETURNS boolean
    LANGUAGE sql STABLE AS $$
    SELECT count(DISTINCT chip) <= 1 FROM (
        SELECT lower(chip_vendor) || '/' || lower(chip_model) AS chip FROM reports
        WHERE id_hashes @> jsonb_build_object(k, v)
        UNION ALL
        SELECT chip FROM report_star_cameras WHERE key = k || ':' || v
    ) carriers
$$;

-- What was paid before this migration, while the reports still say it.
INSERT INTO report_star_cameras (report_id, key, chip)
SELECT DISTINCT r.id, h.key || ':' || h.value, lower(r.chip_vendor) || '/' || lower(r.chip_model)
FROM reports r JOIN report_stars s ON s.report_id = r.id
CROSS JOIN LATERAL jsonb_each_text(r.id_hashes) h
WHERE h.key IN ('mac', 'die_id', 'cloud_id') AND h.value ~ '^[0-9a-f]+$'
ON CONFLICT DO NOTHING;

INSERT INTO report_star_parts (report_id, position, class)
SELECT DISTINCT s.report_id, s.position,
       CASE WHEN s.position = 0 THEN 'yaml' ELSE 'dump' END
FROM report_stars s JOIN reports r ON r.id = s.report_id
LEFT JOIN report_files f ON f.report_id = s.report_id AND f.position = s.position
WHERE (s.position = 0 AND r.yaml <> '') OR f.kind = 'backup'
ON CONFLICT DO NOTHING;
