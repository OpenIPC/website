-- What OpenIPC's CI builds, pushed once per build by the CI itself
-- (internal/builds/PUSH.md). Nothing here is polled from GitHub: a row exists
-- because the build that made it said so, over an OIDC-verified request.
--
-- Rows, not documents. The size reports and kconfig graphs arrive as JSON and
-- are parsed into these tables; no JSON is kept.

CREATE TYPE build_source   AS ENUM ('firmware', 'builder', 'uboot');
CREATE TYPE storage_medium AS ENUM ('nor', 'nand', 'emmc', 'sd');

CREATE TABLE builds (
    id            text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9._-]{1,128}$'),
    source        build_source NOT NULL,
    -- The release tag the assets download from: the dated tag for firmware
    -- and builder, `latest` for u-boot.
    release       text NOT NULL CHECK (release ~ '^[A-Za-z0-9._-]{1,128}$'),
    sha           text NOT NULL CHECK (sha ~ '^[0-9a-f]{40}$'),
    built_at      timestamptz NOT NULL,
    published_at  timestamptz NOT NULL,
    webui_digest  text,
    -- Which workflow run pushed it, from the verified token; for the log and
    -- for answering "where did this come from".
    pushed_by     text NOT NULL,
    ingested_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX builds_by_source ON builds (source, built_at DESC, id DESC);

CREATE TABLE build_assets (
    build_id  text   NOT NULL REFERENCES builds (id) ON DELETE CASCADE,
    name      text   NOT NULL CHECK (name ~ '^[A-Za-z0-9._+-]{1,200}$'),
    size      bigint NOT NULL CHECK (size > 0),
    sha256    text   NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    -- Parsed from openipc.<board>-<storage>-<edition>.tgz when the name has
    -- that shape; NULL for u-boot and for builder's device tarballs.
    board     text,
    storage   storage_medium,
    edition   text,
    PRIMARY KEY (build_id, name)
);
-- The index is "the newest retained build that published each name".
CREATE INDEX build_assets_by_name ON build_assets (name);

CREATE TABLE build_aliases (
    build_id  text NOT NULL REFERENCES builds (id) ON DELETE CASCADE,
    chip      text NOT NULL CHECK (chip  ~ '^[a-z0-9]+$'),
    model     text NOT NULL CHECK (model ~ '^[a-z0-9]+$' AND model <> chip),
    PRIMARY KEY (build_id, chip)
);

-- One per build and platform (board-variant for firmware, device for
-- builder): size_report.py's document, as rows.
CREATE TABLE platform_reports (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    build_id               text NOT NULL REFERENCES builds (id) ON DELETE CASCADE,
    platform               text NOT NULL CHECK (platform ~ '^[A-Za-z0-9._-]{1,128}$'),
    board                  text,
    variant                text,
    flash_mb               integer,
    kernel_version         text,
    kernel_image_path      text,
    kernel_uimage_bytes    bigint,
    kernel_vmlinux_bytes   bigint,
    kernel_used_kb         integer,
    kernel_cap_kb          integer,
    rootfs_used_kb         integer,
    rootfs_cap_kb          integer,
    rootfs_uncompressed    bigint,
    rootfs_compressed      bigint,
    rootfs_compression     text,
    UNIQUE (build_id, platform)
);
CREATE INDEX platform_reports_by_platform ON platform_reports (platform, build_id);

CREATE TABLE report_packages (
    report_id            bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    name                 text   NOT NULL,
    uncompressed_bytes   bigint NOT NULL,
    compressed_bytes     bigint,
    file_count           integer,
    PRIMARY KEY (report_id, name)
);
-- Trends: one package on one platform across builds.
CREATE INDEX report_packages_by_name ON report_packages (name);

CREATE TABLE report_package_files (
    report_id  bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    package    text   NOT NULL,
    path       text   NOT NULL,
    bytes      bigint NOT NULL,
    PRIMARY KEY (report_id, package, path)
);

CREATE TABLE report_modules (
    report_id   bigint  NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    name        text    NOT NULL,
    path        text    NOT NULL,
    bytes       bigint  NOT NULL,
    package     text,
    autoloaded  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (report_id, path)
);

CREATE TABLE report_builtins (
    report_id  bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    name       text   NOT NULL,
    PRIMARY KEY (report_id, name)
);

CREATE TABLE report_autoload (
    report_id  bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    name       text   NOT NULL,
    PRIMARY KEY (report_id, name)
);

CREATE TABLE report_removed (
    report_id     bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    path          text   NOT NULL,
    package       text,
    source_bytes  bigint,
    PRIMARY KEY (report_id, path)
);

-- kconfig_graph.py's graph and help, per build and platform.
CREATE TABLE kconfig_symbols (
    report_id        bigint NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    symbol           text   NOT NULL,
    package          text,
    type             text,
    prompt           text,
    direct_dep_expr  text,
    help             text,
    PRIMARY KEY (report_id, symbol)
);

CREATE TYPE kconfig_relation AS ENUM ('depends_on', 'selects', 'selected_by');
CREATE TABLE kconfig_edges (
    report_id  bigint           NOT NULL REFERENCES platform_reports (id) ON DELETE CASCADE,
    symbol     text             NOT NULL,
    relation   kconfig_relation NOT NULL,
    other      text             NOT NULL,
    PRIMARY KEY (report_id, symbol, relation, other)
);
