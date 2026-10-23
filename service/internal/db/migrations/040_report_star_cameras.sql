-- Stars per camera, not per report (reports/camera.go): what a member's
-- reports about one camera hold of ipctool's output and of the best dump is
-- paid once. A takedown blanks a report's identifiers and deletes its files
-- but keeps the stars it was paid (the owner's privacy, not a penalty), so
-- what made it the camera's and which of its parts were the camera's share
-- is written here when its stars are, and outlives the takedown: a later
-- report of the same camera is paid only what the withdrawn one was not.
--
-- Keyed hashes only, as reports.id_hashes holds them ("mac:<hash>"); never an
-- identifier in clear. Inserted, never changed, guarded as the reports are.
CREATE TABLE report_star_cameras (
    report_id text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    key       text NOT NULL CHECK (key ~ '^(mac|die_id|cloud_id):[0-9a-f]+$'),
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

-- What was paid before this migration, while the reports still say it.
INSERT INTO report_star_cameras (report_id, key)
SELECT DISTINCT r.id, h.key || ':' || h.value
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
