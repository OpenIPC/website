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
  /** A photo a source shows for other boards too: how many boards show it, this one included. */
  shared?: number;
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
  /** A finished device's boards: confirmed by an owner, or most likely from the vendor's firmware. */
  contents?: Content[];
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
  /** The builds a maker whose firmware is keyed by board model (Anjoy Vision) made for this board, newest first. */
  firmware?: ModelBuild[];
  /** For each of the board's shared photos (by sha256), the other boards shown with it. */
  shared_photos?: Record<string, { id: string; model: string | null }[]>;
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
  /** The seller's archive the file was mirrored from (cctvsp.ru), when it is not the vendor's own download. */
  origin?: string | null;
  /** The file's page in that archive. */
  origin_url?: string | null;
}

/**
 * What an XM device ID can be flashed with: every stock build the vendor
 * published, newest first (OpenIPC/xmupdates), and the image that moves it
 * to OpenIPC (OpenIPC/coupler).
 */
/** One board a finished device holds, as the tree gives it. */
export interface Content {
  /** The board as the evidence names it. */
  code: string;
  /** The catalogue's board of that code; null until it lists one. */
  board_id: string | null;
  status: 'likely' | 'confirmed';
  /** firmware_page: the vendor's page names the board; firmware_build: the firmware is built for its module; owner: a photo. */
  basis: 'firmware_page' | 'firmware_build' | 'owner';
  evidence: string;
  /** What the evidence shows: the firmware file's name. */
  label: string | null;
  source: string;
}

/** A firmware build for a board model, as the maker names it. */
export interface ModelBuild extends VendorFirmware {
  /** What it is for: MCA31_V0_BU_LIGHT is module MC-A31, hardware revision V0, a flavour. */
  device_type: string;
  /** "public" for the maker's own build, else the customer's tag. */
  app: string;
  category?: string | null;
  /** A collection's variant folder, in the reader's language. */
  variant?: string | null;
  /** The collection it came from ("pre-2022"), for builds kept apart from the upgrade server. */
  collection?: string | null;
}

export interface VendorDevice {
  id: string;
  stock: VendorFirmware[];
  /** A seller's builds (cctvsp.ru's IPeye builds), only when the vendor has none. */
  sellers?: VendorFirmware[];
  coupler: VendorFirmware | null;
}

/** GET /api/v1/vendor-firmware/{deviceId}. */
export interface DeviceAnswer {
  device: VendorDevice;
  /** The catalogue entries that run the device ID: boards and finished devices. */
  boards: { id: string; model: string | null; kind?: string }[];
}
