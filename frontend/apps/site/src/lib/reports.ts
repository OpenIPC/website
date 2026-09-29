/**
 * Owner reports, as the site's own API answers them (service/internal/reports):
 * a receipt at /api/v1/reports/{id}, and a board's published reports at
 * /api/v1/reports?model=. Identifiers in them are keyed hashes already.
 */
export type ReportState = 'pending' | 'published' | 'rejected' | 'withdrawn';

export interface ReportFile {
  kind: 'backup' | 'photo' | 'boot_log' | 'uboot_env' | 'note' | 'document';
  name: string;
  bytes: number;
  sha256?: string;
  url?: string;
  private?: boolean;
}

export interface ReportFacts {
  chip_vendor?: string;
  chip_model?: string;
  sensor?: string;
  flash_id?: string;
  flash_size?: string;
  board_vendor?: string;
  board_model?: string;
  main_app?: string;
}

export interface Report {
  id: string;
  received_at: string;
  channel: 'ipctool' | 'agent' | 'web';
  status: ReportState;
  reviewed_at?: string;
  backup_consent: 'none' | 'private' | 'public';
  facts?: ReportFacts;
  yaml?: string;
  tool?: string;
  note?: string;
  files: ReportFile[];
  models: { id: string; model: string; manufacturer: string }[];
  same_board: number;
  guess?: { model_id: string; model: string; manufacturer: string; url: string; why: string[] };
}

export const RECEIPT_ID = /^r-[a-z0-9]{8}$/;

export class NotFound extends Error {}

async function json<T>(url: string): Promise<T> {
  const r = await fetch(url, { headers: { Accept: 'application/json' } });
  if (r.status === 404) throw new NotFound(url);
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return (await r.json()) as T;
}

export function fetchReport(id: string): Promise<Report> {
  return json<Report>(`/api/v1/reports/${encodeURIComponent(id)}`);
}

export function fetchBoardReports(model: string): Promise<{ reports: Report[] }> {
  return json<{ reports: Report[] }>(`/api/v1/reports?${new URLSearchParams({ model }).toString()}`);
}

/** "HiSilicon 3516CV300 · Sony IMX291 · 8M" */
export function summary(f: ReportFacts | undefined): string {
  if (!f) return '';
  return [[f.chip_vendor, f.chip_model].filter(Boolean).join(' '), f.sensor, f.flash_size].filter(Boolean).join(' · ');
}

export function size(bytes: number): string {
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}

/** uget's release builds (OpenIPC/uget v0.1.0), vendored under /uget/, by the C library each links. */
export const UGET_BUILDS: { file: string; libc: 'uclibc' | 'glibc'; toolchain: string }[] = [
  { file: 'uget.arm-himix100-linux.sh', libc: 'uclibc', toolchain: 'himix100' },
  { file: 'uget.arm-hisiv500-linux.sh', libc: 'uclibc', toolchain: 'hisiv500' },
  { file: 'uget.arm-hisiv510-linux.sh', libc: 'uclibc', toolchain: 'hisiv510' },
  { file: 'uget.arm-hisiv300-linux.sh', libc: 'uclibc', toolchain: 'hisiv300' },
  { file: 'uget.arm-himix200-linux.sh', libc: 'glibc', toolchain: 'himix200' },
  { file: 'uget.arm-hisiv600-linux.sh', libc: 'glibc', toolchain: 'hisiv600' },
];
