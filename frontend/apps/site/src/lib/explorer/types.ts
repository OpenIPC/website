// The documents the explorer reads, as the Go service answers them
// (service/internal/builds/explorer.go). The size report and kconfig shapes
// are size_report.py's and kconfig_graph.py's, reassembled from the database.

export const SOURCES = ["firmware", "builder"] as const;
export type Source = (typeof SOURCES)[number];

export type Build = {
  id: string;
  sha: string;
  short: string;
  built_at: string;
  platforms: string[];
};

export type IndexFile = {
  schema: 1;
  source: Source;
  builds: Build[];
  /** Platforms whose newest build carries a kconfig graph. */
  kconfig_available_for: string[];
};

// kconfig_graph.py's graph document.
export type KconfigSymbol = {
  package: string | null;
  type: "bool" | "tristate" | "string" | "int" | "hex" | string;
  prompt: string | null;
  depends_on: string[];
  selects: string[];
  // Filtered to currently-set selectors only — direct hard-pin sources.
  selected_by: string[];
  direct_dep_expr: string;
};

export type KconfigGraph = {
  schema: number;
  board: string;
  variant: string;
  br_ver: string;
  symbol_prefix: string;
  symbol_count: number;
  skipped_no_node: number;
  symbols: Record<string, KconfigSymbol>;
};

export type KconfigHelp = {
  schema: number;
  board: string;
  variant: string;
  help: Record<string, string>;
};

export type SizesPackage = {
  name: string;
  uncompressed_bytes: number;
  compressed_bytes_approx: number | null;
  file_count: number | null;
  top_files: Array<{ path: string; bytes: number }>;
};

export type SizesModule = {
  name: string;
  path: string;
  bytes: number;
  package: string | null;
  autoloaded: boolean;
};

export type SizesRemoved = {
  path: string;
  package: string | null;
  source_bytes: number | null;
};

export type Sizes = {
  schema: number;
  board: string;
  variant: string;
  flash_mb: number | null;
  kernel_version: string | null;
  rootfs: {
    uncompressed_bytes: number;
    compressed_bytes: number | null;
    compression: string | null;
    compression_ratio: number | null;
  };
  kernel: {
    image_path: string | null;
    uimage_bytes: number | null;
    vmlinux_bytes: number | null;
  };
  headroom: {
    kernel: { used_kb: number | null; cap_kb: number | null; headroom_kb: number | null };
    rootfs: { used_kb: number | null; cap_kb: number | null; headroom_kb: number | null };
  };
  packages: SizesPackage[];
  linux_components: {
    kernel_image: {
      image_path: string | null;
      uimage_bytes: number | null;
      vmlinux_bytes: number | null;
    };
    modules: SizesModule[];
    built_in: string[];
    autoload_list: string[];
  };
  removed_by_finalize: SizesRemoved[];
  /** The tarballs the build published for this platform, added by the service. */
  downloads?: SizesDownload[];
};

export type SizesDownload = {
  name: string;
  size: number;
  sha256: string;
  storage: string | null;
  url: string;
};

// The firmware-drift report Builder pushes once a day, as
// GET /api/v1/explorer/builder/upstream answers it (service/internal/drift).

export type UpstreamCommit = { sha: string; date: string; author: string; subject: string };

export type UpstreamShadow = {
  /** The file under devices/ that builder.sh copies over firmware's. */
  builder: string;
  firmware: string;
  status: "ok" | "unpinned" | "missing_builder" | "firmware_gone" | "moved";
  pinned_blob?: string;
  current_blob?: string;
  /** The firmware commit the pin was reconciled against, when known. */
  pinned_commit?: string;
  pin_unknown?: boolean;
  truncated?: boolean;
  reconciled?: string;
  note?: string;
  devices: string[];
  /** Firmware's changes to the file since the pin, newest first. */
  commits?: UpstreamCommit[];
  /** Open since: the oldest report in the unbroken run that had it. */
  since?: string;
};

export type UpstreamSymbol = {
  symbol: string;
  kind: "dead" | "known_dead" | "stray" | "firmware_retired" | "stale_entry";
  reason?: string;
  allowed?: string[];
  devices: string[];
  since?: string;
};

export type UpstreamDevice = { device: string; dir: string; shadows: number; attention: number; symbols: number };

export type UpstreamReport = {
  schema: 1;
  report: {
    checked_at: string;
    received_at: string;
    builder_commit: string;
    firmware_commit: string;
    buildroot_version: string;
    run_url: string;
    retained: number;
    oldest_checked_at: string;
  };
  devices: UpstreamDevice[];
  shadows: UpstreamShadow[];
  symbols: UpstreamSymbol[];
  notices: string[];
};
