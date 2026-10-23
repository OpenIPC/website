-- The owner report whose publishing had a board answer to a marking
-- (boards.AddMarkings: what a reviewer read on the report's photos), so that
-- rejecting or taking down that report takes the marking back, as it takes
-- back a board the publish added (created_by_report, migration 033). Null
-- for every alias an importer wrote, for a board's own marking, and for a
-- marking the board answered to before the report came.
ALTER TABLE board_model_aliases ADD COLUMN added_by_report text REFERENCES reports ON DELETE RESTRICT;
CREATE INDEX board_model_aliases_by_report ON board_model_aliases (added_by_report) WHERE added_by_report IS NOT NULL;
