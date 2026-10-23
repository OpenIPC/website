-- How sure a report's link to a board is, and why. A link used to read as
-- fact, and some are inferences: r-nw2j9a8k (XM device ID 00018510) went to
-- IPG-50HV10PT-S because the same member's other camera on its platform is
-- there, while that device ID is five catalogue boards -- probable until a
-- photo of the PCB settles it.
--
--   confirmed  the board itself says so: a sticker or silkscreen read, the
--              device ID in its firmware naming one board
--   probable   a good inference: the platform ipctool names, a sibling report
--   guess      the best there is, to be confirmed
--
-- Every link made so far was made as fact, and code that names no certainty
-- (the image before this one) still makes one: the default is 'confirmed'.
-- Evidence is the reviewer's few words, shown beside a link that is not
-- confirmed ("same platform as r-3yi3z6jw"). It is stored as published:
-- redacted as the report's note is (reports.Redact), the board's
-- identifiers and any MAC replaced by keyed hashes -- at most 200
-- characters as written, longer once a MAC becomes its placeholder.
--
-- A link's certainty changes only through reports.Store.SetCertainty, which
-- stands the guard down for that one statement: the trigger of migration 016
-- still refuses any other UPDATE.
ALTER TABLE report_models
    ADD COLUMN certainty text NOT NULL DEFAULT 'confirmed' CHECK (certainty IN ('confirmed', 'probable', 'guess')),
    ADD COLUMN evidence  text NOT NULL DEFAULT '' CHECK (length(evidence) <= 1000);

-- The unit a published report becomes on its board (boards.ApplyReportUnits)
-- carries its link's certainty, so the board's files say it too. Null for
-- every unit that is not a report's.
ALTER TABLE board_units
    ADD COLUMN link_certainty text CHECK (link_certainty IN ('confirmed', 'probable', 'guess')),
    ADD COLUMN link_evidence  text NOT NULL DEFAULT '';
