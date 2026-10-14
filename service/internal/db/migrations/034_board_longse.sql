-- Longse (also Herospeed, its firmware house): the boards its IP cameras are
-- built on, from Herospeed's firmware lists (tools/board-donors/herospeed.py),
-- and the cameras themselves, from Longse's catalogue (longse.py). Their
-- source rows are written by the import, as every donor's are.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'herospeed';
ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'longse';

-- A Longse camera's model name is its housing and its board: DMSAKL5RC is
-- housing DMSA on board KL5RC. That is a third kind of evidence for what is
-- inside, 'model_code', likely as the firmware's is.
ALTER TABLE board_contents DROP CONSTRAINT board_contents_basis_check;
ALTER TABLE board_contents ADD CONSTRAINT board_contents_basis_check
    CHECK (basis IN ('firmware_page', 'firmware_build', 'model_code', 'owner'));

-- Herospeed's builds, mirrored by OpenIPC/herospeedupdates, keyed by board
-- model as Anjoy Vision's are (migration 013).
ALTER TABLE vendor_firmware DROP CONSTRAINT vendor_firmware_source_check;
ALTER TABLE vendor_firmware ADD CONSTRAINT vendor_firmware_source_check
    CHECK (source IN ('xmupdates', 'coupler', 'anjoyupdates', 'herospeedupdates'));
