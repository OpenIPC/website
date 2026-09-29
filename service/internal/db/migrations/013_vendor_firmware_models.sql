-- Firmware keyed by a board model rather than an XM device ID: Anjoy Vision's
-- builds (OpenIPC/anjoyupdates) each name the board and build they are for
-- (DeviceType MCA31_V0_BU_LIGHT: module MC-A31, hardware revision V0, a
-- flavour), and are matched to the catalogue's boards when read.
--
--   device_type  the build's own name for what it is for
--   app          "public" for the maker's own build, else the customer's tag
--   category     camera, wifi, 4g, nvr, dvr
--   module       the module a collection's folder names (the pre-2022 builds)
--   variant      the collection's variant folder, {zh, en, ru}
--   collection   the collection the build came from ("pre-2022"), else null
ALTER TABLE vendor_firmware
    ALTER COLUMN device_id DROP NOT NULL,
    ADD COLUMN device_type text,
    ADD COLUMN app text,
    ADD COLUMN category text,
    ADD COLUMN module text,
    ADD COLUMN variant jsonb,
    ADD COLUMN collection text,
    ADD CONSTRAINT vendor_firmware_keyed CHECK (device_id IS NOT NULL OR device_type IS NOT NULL);

-- The source list migration 007 fixed gains anjoyupdates.
ALTER TABLE vendor_firmware DROP CONSTRAINT vendor_firmware_source_check;
ALTER TABLE vendor_firmware ADD CONSTRAINT vendor_firmware_source_check
    CHECK (source IN ('xmupdates', 'coupler', 'anjoyupdates'));
