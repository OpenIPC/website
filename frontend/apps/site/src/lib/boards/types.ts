/**
 * The board catalogue as the Go service answers it: GET /api/v1/boards,
 * GET /api/v1/boards/models/{id} and GET /api/v1/boards/search
 * (service/internal/boards/api.go).
 */

export type FileKind =
  | 'photo_front' | 'photo_back' | 'photo_other' | 'pinout'
  | 'flash_dump' | 'uboot_env' | 'boot_log' | 'document' | 'note'
  /** A seller-hosted stock firmware file. */
  | 'firmware';

export interface BoardFile {
  kind: FileKind;
  name: string;
  url: string;
  thumb_url?: string;
  mime: string;
  bytes: number;
  sha256: string;
  width?: number;
  height?: number;
  lines?: number;
}

export interface Unit {
  id: string;
  sensor: string | null;
  flash_chip: string | null;
  flash_size_mb: number | null;
  source: string;
  source_ref: string;
  contributed_by: string;
  notes: string | null;
  files: BoardFile[];
}

export interface Coverage {
  units: number;
  photos: number;
  pinouts: number;
  flash_dumps: number;
  uboot_envs: number;
  boot_logs: number;
  documents: number;
}

/** What a card says about a board, in the page's language when a source has it. */
export interface Summary {
  name: string | null;
  lead: string | null;
  /** The language served. */
  locale: string;
  /** The language the source wrote it in, when this is a translation. */
  translated_from: string | null;
}

export interface Model {
  id: string;
  model: string | null;
  /** The catalogue urlname, when the SoC is one OpenIPC catalogues. */
  soc: string | null;
  /** The SoC as the source wrote it. */
  soc_label: string | null;
  family: string | null;
  notes: string | null;
  /** The product line, as the source files it ("NVR Board"). */
  category: string | null;
  /** "openipc-ready", "discontinued". */
  tags: string[];
  /** The other codes sources print for this board. */
  aliases?: string[];
  /** board, or the finished device it is (camera, recorder, doorbell, base_station). */
  kind?: string;
  /** The XM device IDs the board runs, with what each can be flashed with. */
  devices?: VendorDevice[];
  /** The year the maker's own catalogue first showed the board, where a source dates it. */
  listed_year?: number | null;
  summary: Summary | null;
  /** Source ids, which `BoardsFile.sources` names. */
  sources: string[];
  coverage: Coverage;
  units: Unit[];
}

export interface Manufacturer {
  id: string;
  name: string;
  aliases: string[];
  website: string | null;
  models: Model[];
}

export interface Source {
  id: string;
  name: string;
  url: string;
  note: string;
  ref: string;
}

export interface BoardsFile {
  schema: number;
  locale: string;
  files_prefix: string;
  sources: Source[];
  manufacturers: Manufacturer[];
}

/** One source's say about a board: GET /api/v1/boards/models/{id}. */
export interface About {
  source: string;
  /** The language actually served. */
  locale: string;
  translated_from: string | null;
  name: string | null;
  /** Paragraphs separated by blank lines. */
  description: string | null;
  /** One feature per line. */
  features: string | null;
  specs: [label: string, value: string][];
}

export type LinkKind = 'stock_firmware' | 'source_page' | 'vendor_page' | 'successor' | 'predecessor' | 'related' | 'pcb' | 'on_pcb';

export interface BoardLink {
  source: string;
  kind: LinkKind;
  label: string;
  url: string | null;
  /** A model id in the catalogue. */
  target: string | null;
}

export interface ModelDetail {
  schema: number;
  locale: string;
  id: string;
  model: string | null;
  tags: string[];
  about: About[];
  links: BoardLink[];
  devices?: VendorDevice[];
}

export type TextKind = 'uboot_env' | 'boot_log' | 'note';

export interface Hit {
  kind: TextKind;
  name: string;
  url: string;
  line: number;
  text: string;
  unit_id: string;
  model_id: string;
  model: string | null;
  soc: string | null;
  family: string | null;
  manufacturer_id: string;
  manufacturer_name: string;
}

export interface SearchResult {
  schema: number;
  query: string;
  kinds: TextKind[];
  soc: string;
  truncated: boolean;
  hits: Hit[];
}

/** A firmware file OpenIPC's own projects publish (xmupdates, coupler). */
export interface VendorFirmware {
  key: string;
  version: string;
  build: string;
  url: string;
  sha256: string | null;
  size: number | null;
  published_at: string | null;
  soc?: string | null;
}

/**
 * What an XM device ID can be flashed with: every stock build the vendor
 * published, newest first (OpenIPC/xmupdates), and the image that moves it
 * to OpenIPC (OpenIPC/coupler).
 */
export interface VendorDevice {
  id: string;
  stock: VendorFirmware[];
  coupler: VendorFirmware | null;
}

/** GET /api/v1/vendor-firmware/{deviceId}. */
export interface DeviceAnswer {
  device: VendorDevice;
  boards: { id: string; model: string | null }[];
}
