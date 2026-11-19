-- coupler's images for cameras with no XM device ID (Rostelecom's), pushed by
-- their own workflow and keyed by board model as Anjoy Vision's builds are
-- (migration 013).
ALTER TABLE vendor_firmware DROP CONSTRAINT vendor_firmware_source_check;
ALTER TABLE vendor_firmware ADD CONSTRAINT vendor_firmware_source_check
    CHECK (source IN ('xmupdates', 'coupler', 'anjoyupdates', 'herospeedupdates', 'coupler-rostelecom'));
