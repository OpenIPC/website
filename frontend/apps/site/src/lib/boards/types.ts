/**
 * The board catalogue as the Go service answers it: GET /api/v1/boards and
 * GET /api/v1/boards/search (service/internal/boards/api.go).
 */

export type FileKind =
  | 'photo_front' | 'photo_back' | 'photo_other' | 'pinout'
  | 'flash_dump' | 'uboot_env' | 'boot_log' | 'document' | 'note';

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

export interface Model {
  id: string;
  model: string | null;
  /** The catalogue urlname, when the SoC is one OpenIPC catalogues. */
  soc: string | null;
  /** The SoC as the source wrote it. */
  soc_label: string | null;
  family: string | null;
  notes: string | null;
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
  ref: string;
}

export interface BoardsFile {
  schema: number;
  files_prefix: string;
  sources: Source[];
  manufacturers: Manufacturer[];
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
