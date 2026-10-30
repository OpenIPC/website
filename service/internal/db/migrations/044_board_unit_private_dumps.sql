-- How many flash dumps an owner report holds that are not public: kept
-- private by their sender, or by a reviewer because they hold what
-- identifies the camera. The board shows that a dump exists without
-- serving it; a public one is listed as a file of the unit, served from
-- the report (no-store), never copied under BOARDS_ROOT. Zero for every
-- unit but a report's.
ALTER TABLE board_units ADD COLUMN IF NOT EXISTS private_dumps int NOT NULL DEFAULT 0 CHECK (private_dumps >= 0);

-- A file served from elsewhere: a report's public flash dump, at its
-- report's address. Its path names it in the unit as any file's does, and
-- nothing is stored there.
ALTER TABLE board_artifacts ADD COLUMN IF NOT EXISTS href text
    CHECK (href ~ '^/api/v1/reports/r-[a-z0-9]{8}/files/[0-9]+\?v=[0-9a-f]{12}$');
