-- A photo shared between boards is found by its bytes: the tree counts, per
-- source, the boards that show each photo's sha256, and a board's detail
-- lists the others. Without an index each lookup scans every artifact.
CREATE INDEX IF NOT EXISTS board_artifacts_by_sha256 ON board_artifacts (sha256);
