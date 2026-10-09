/**
 * Kernel crashes cameras recovered from, as the service answers them
 * (service/internal/crashes): the public list of signatures at
 * /api/v1/crashes, and under /api/v1/club the member's own crashes and the
 * maintainers' triage. A signature is the top of a crash's backtrace: one bug,
 * however many cameras sent it. majestic's own crashes (kind signal, class
 * user) are the maintainers' only: the public list never has them.
 */
import { ClubError } from './club';

export type Kind = 'bootloop' | 'panic' | 'oops' | 'bug' | 'signal' | 'warning';
export type Status = 'open' | 'confirmed' | 'fixed' | 'wontfix' | 'bogus';

/**
 * A function in a backtrace. A frame of majestic's says where in its source;
 * a probable one was found by scanning the stack past where the unwinder
 * stopped.
 */
export interface Frame { fn: string; module?: string; file?: string; line?: number; probable?: boolean }

export interface Signature {
  id: string;
  class: 'fatal' | 'warning' | 'user';
  kind: Kind;
  title: string;
  frames: Frame[];
  status: Status;
  fixed_in?: string;
  issue_url?: string;
  in_irq: boolean;
  events: number;
  cameras: number;
  socs: string[];
  sensors: string[];
  firmware: string[];
  first_seen: string;
  last_seen: string;
  /** Seen on a kernel built within two weeks of the newest firmware. */
  current: boolean;
  score: number;
  merged_into?: string;
  note?: string;
}

export interface Combo {
  soc: string;
  sensor: string;
  kernel?: string;
  kernel_built?: string;
  firmware?: string;
  majestic?: string;
  events: number;
  cameras: number;
}

export interface Trace {
  kind: Kind;
  reason: string;
  comm?: string;
  pc?: string;
  in_irq?: boolean;
  frames: Frame[];
  interrupted?: Frame[];
}

/** One crash, as a maintainer reads it to reproduce it. */
export interface Detail {
  id: string;
  signature: string;
  received_at: string;
  channel: string;
  kind: Kind;
  self_inflicted: boolean;
  firmware: string;
  majestic: string;
  soc: string;
  sensor: string;
  board: string;
  machine: string;
  kernel: string;
  kernel_build: string;
  kernel_built?: string;
  cmdline: string;
  uptime?: number;
  modules: string[];
  fatal: Trace;
  before: Trace[];
  anomalies: Record<string, number>;
  leadup: string[];
  meta?: unknown;
  log?: string;
  builds?: { id: string; release: string; sha: string; built_at: string }[];
  /** A majestic crash's backtrace, or why it has none yet. */
  symbolization?: Symbolization;
}

export interface Symbolization {
  status: 'pending' | 'done' | 'failed';
  attempts: number;
  frames: Frame[];
  sources?: Record<string, unknown>;
  error?: string;
  at: string;
}

export interface Mine {
  id: string;
  signature: string;
  title: string;
  kind: Kind;
  status: Status;
  soc: string;
  sensor: string;
  received_at: string;
  self_inflicted: boolean;
  camera: boolean;
}

export interface Rules { report: number; first: number; fixed: number; month_cap: number }

/** What a sent crash answers. */
export interface Sent {
  id: string;
  signature: string;
  title: string;
  kind: Kind;
  duplicate: boolean;
  self_inflicted: boolean;
  url: string;
}

async function call<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin', ...init, headers: { Accept: 'application/json', ...(init?.headers ?? {}) } });
  let body: unknown = null;
  try { body = await r.json(); } catch { /* not JSON */ }
  if (!r.ok) throw new ClubError(r.status, (body as { error?: string } | null)?.error ?? `HTTP ${r.status}`);
  return body as T;
}

export const fetchSignatures = () => call<{ signatures: Signature[] }>('/api/v1/crashes');

export const fetchSignature = (id: string) =>
  call<{ signature: Signature; seen_on: Combo[] }>(`/api/v1/crashes/${encodeURIComponent(id)}`);

export const fetchMyCrashes = () => call<{ crashes: Mine[]; stars: number; rules: Rules }>('/api/v1/club/crashes');

/** The member's upload of the crash log their WebUI downloaded. */
export const sendCrash = (form: FormData) => call<Sent>('/api/v1/club/crashes', { method: 'POST', body: form });

export const fetchTriage = () => call<{ signatures: Signature[] }>('/api/v1/club/crashes/triage');

export const fetchCrashDetail = (id: string) =>
  call<{ signature: Signature; seen_on: Combo[]; crashes: Detail[] }>(`/api/v1/club/crashes/${encodeURIComponent(id)}`);

export interface Triage { status: Status; fixed_in?: string; issue_url?: string; merge_into?: string; note?: string }

export const decideCrash = (id: string, t: Triage) =>
  call<{ status: Status; stars_taken_back: number }>(`/api/v1/club/crashes/${encodeURIComponent(id)}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(t),
  });

/** A frame as a backtrace prints it: fn [module]. */
export const frame = (f: Frame) => (f.module ? `${f.fn} [${f.module}]` : f.fn);

/** A frame with where in the source it is, when that is known. */
export const frameAt = (f: Frame) => (f.file ? `${frame(f)}  ${f.file}:${f.line ?? 0}` : frame(f));

export const KIND_TONE: Record<Kind, string> = {
  bootloop: 'bg-[#fbe9ea] text-[#a3262e]',
  panic: 'bg-[#fbe9ea] text-[#a3262e]',
  oops: 'bg-[#fff4e2] text-[#8a4b00]',
  bug: 'bg-[#fff4e2] text-[#8a4b00]',
  signal: 'bg-[#fff4e2] text-[#8a4b00]',
  warning: 'bg-surface-alt text-body-secondary',
};

export const STATUS_TONE: Record<Status, string> = {
  open: 'border border-dashed border-hairline bg-surface-alt text-body-secondary',
  confirmed: 'bg-[#eef0fc] text-[#3d4dad]',
  fixed: 'bg-[#e3f5ec] text-[#146c3c]',
  wontfix: 'bg-surface-alt text-body-secondary',
  bogus: 'bg-[#fbe9ea] text-[#a3262e]',
};
