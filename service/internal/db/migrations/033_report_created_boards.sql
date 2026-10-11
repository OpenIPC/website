-- The owner report whose publishing added a board (boards.CreateModel), so
-- that rejecting that report takes the board back once nothing else is on
-- it (#452). Rejecting a report used to leave the board its publish made in
-- the catalogue, empty, under a marking nobody's board has.
--
-- The boards made so far are filled in from the report linked first: a
-- publish adds the board and links the report in one transaction, so no
-- other link to it can be earlier. A board none of whose links survived
-- (an edit or a takedown removed them) gets no creator and is never swept;
-- there was none when this was written (four club boards on production and
-- on dev, each linked, 2026-10-11), and `openipc boards remove` takes one out.
ALTER TABLE board_models ADD COLUMN created_by_report text REFERENCES reports ON DELETE RESTRICT;

UPDATE board_models m SET created_by_report = first.report_id
FROM (
    SELECT DISTINCT ON (rm.model_id) rm.model_id, rm.report_id
    FROM report_models rm
    WHERE EXISTS (SELECT 1 FROM board_model_aliases a WHERE a.model_id = rm.model_id AND a.source = 'club')
    ORDER BY rm.model_id, rm.at, rm.report_id
) first
WHERE m.id = first.model_id;
