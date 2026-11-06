-- Hikvision (海康威视): its network cameras from its product pages in the
-- Wayback Machine (tools/board-donors/hikvision_snapshot.py). Its source row
-- is written by the import, as every donor's is.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'hikvision';

-- The lab's DS-2CD2423G0-I reached production through an owner's report before
-- this snapshot, so the review made the maker, with a sender's name and none
-- of the importer's. An import leaves a maker that exists as it is.
UPDATE board_manufacturers SET aliases = ARRAY['海康威视', 'HiLook'], position = 10 WHERE id = 'hikvision';
