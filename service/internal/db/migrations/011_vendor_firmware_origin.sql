-- Where a pushed file came from when it is not the vendor's own download
-- page: xmupdates also mirrors sellers' firmware archives (cctvsp.ru's
-- /support/proshivki keeps builds the vendor has withdrawn, and some of its
-- own, such as IPeye versions). The site names the archive on each such file.
ALTER TABLE vendor_firmware
    ADD COLUMN origin text CHECK (origin ~ '^[a-z0-9][a-z0-9.-]{0,63}$'),
    ADD COLUMN origin_url text CHECK (origin_url ~ '^https?://'),
    ADD CONSTRAINT vendor_firmware_origin_named CHECK (origin_url IS NULL OR origin IS NOT NULL);
