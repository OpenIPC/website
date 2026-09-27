-- Firmware for Xiongmai devices, keyed by the XM device ID: the 8-character
-- firmware number a camera reports in its System version
-- (V5.00.R02.000559A7.10010...). Two OpenIPC projects push it (vendorfw/PUSH.md):
--
--   xmupdates  stock firmware, mirrored from the vendor, for those staying on it
--   coupler    the image that moves a device from stock to OpenIPC
--
-- A push replaces its source's rows, so the table is what each project last
-- published. board_device_ids says which boards run a device ID, with the
-- source that says so; a board with a coupler image counts as OpenIPC-ready.

CREATE TABLE vendor_firmware (
    source       text NOT NULL CHECK (source IN ('xmupdates', 'coupler')),
    key          text NOT NULL,
    version      text NOT NULL,
    device_id    text NOT NULL CHECK (device_id ~ '^[0-9A-Z]{8}$'),
    build        text NOT NULL,
    asset_url    text NOT NULL,
    sha256       text CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size         bigint CHECK (size >= 0),
    published_at timestamptz,
    soc          text,
    pushed_by    text NOT NULL,
    pushed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, key, version)
);
CREATE INDEX vendor_firmware_device ON vendor_firmware (device_id, source);

CREATE TABLE board_device_ids (
    model_id  text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    device_id text NOT NULL CHECK (device_id ~ '^[0-9A-Z]{8}$'),
    source    text NOT NULL REFERENCES board_sources (id),
    evidence  text,
    PRIMARY KEY (model_id, device_id, source)
);
CREATE INDEX board_device_ids_device ON board_device_ids (device_id);
