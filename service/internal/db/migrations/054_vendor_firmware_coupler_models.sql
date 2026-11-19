-- coupler's images for cameras with no XM device ID (Anjoy's, Huishi's,
-- Rostelecom's, Tiandy's), each pushed by its own workflow and keyed by board
-- model as Anjoy Vision's builds are (migration 013).
ALTER TABLE vendor_firmware DROP CONSTRAINT vendor_firmware_source_check;
ALTER TABLE vendor_firmware ADD CONSTRAINT vendor_firmware_source_check
    CHECK (source IN ('xmupdates', 'coupler', 'anjoyupdates', 'herospeedupdates',
                      'coupler-anjoy', 'coupler-huishi', 'coupler-rostelecom', 'coupler-tiandy'));
