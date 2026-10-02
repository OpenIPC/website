/**
 * The OpenIPC Club, as the site's own API answers it (service/internal/club):
 * who this browser is signed in as, the ways in, the member's own reports
 * and the maintainers' review queue.
 *
 * The session cookie's path is /api/v1/club, so the pages never carry it and
 * stay cached for everyone; a page learns who is signed in by asking
 * /api/v1/club/me. To spare every visitor that request, a browser that has
 * signed in remembers so in localStorage, and only then does the header ask.
 */
import type { ReportFile, ReportState } from './reports';

export interface Identity { provider: 'telegram' | 'github' | 'email'; handle: string; chat?: boolean }

export interface Member {
  id: string;
  name: string;
  maintainer: boolean;
  quiet: boolean;
  identities: Identity[];
  stars: number;
  pending: number;
}

export interface Me {
  member: Member | null;
  sign_in: { telegram: string | null; github: boolean; email: boolean };
}

export interface MemberFile {
  position: number;
  kind: ReportFile['kind'];
  name: string;
  bytes: number;
  private?: boolean;
  url: string;
  points: number;
}

export interface BoardRef { id: string; model: string; manufacturer: string }

export interface MemberReport {
  id: string;
  received_at: string;
  status: ReportState;
  reviewed_at?: string;
  note?: string;
  board?: BoardRef;
  chip?: string;
  files: MemberFile[];
  stars: number;
  pending: number;
  duplicate?: boolean;
}

export interface Queued {
  id: string;
  received_at: string;
  channel: string;
  status: ReportState;
  chip: string;
  sensor: string;
  board: BoardRef | null;
  models: string[];
  backup_consent: string;
  note?: string;
  tool?: string;
  yaml?: string;
  member?: string;
  guess?: { model_id: string; model: string; manufacturer: string };
  file_list: MemberFile[];
  potential: number;
}

const BASE = '/api/v1/club';
const FLAG = 'openipc.club';

export class ClubError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(BASE + path, { credentials: 'same-origin', ...init, headers: { Accept: 'application/json', ...(init?.headers ?? {}) } });
  let body: unknown = null;
  try { body = await r.json(); } catch { /* an empty or HTML answer */ }
  if (!r.ok) throw new ClubError(r.status, (body as { error?: string } | null)?.error ?? `HTTP ${r.status}`);
  return body as T;
}

const post = <T>(path: string, data?: unknown) => call<T>(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: data === undefined ? undefined : JSON.stringify(data),
});

/** The flag the header reads; storage can be refused, and then it is just absent. */
export function remembered(): boolean {
  try { return localStorage.getItem(FLAG) === '1'; } catch { return false; }
}

function remember(on: boolean) {
  try {
    if (on) localStorage.setItem(FLAG, '1');
    else localStorage.removeItem(FLAG);
  } catch { /* private mode */ }
}

export async function fetchMe(): Promise<Me> {
  const me = await call<Me>('/me');
  remember(me.member !== null);
  return me;
}

export const startTelegram = () => post<{ link: string; expires_at: string }>('/telegram');

export type Poll =
  | { state: 'none' | 'expired' }
  | { state: 'pending'; expires_at: string }
  | { state: 'signed_in'; member: Member };

export async function pollLogin(): Promise<Poll> {
  const p = await call<Poll>('/login');
  if (p.state === 'signed_in') remember(true);
  return p;
}

export const startEmail = (email: string, locale: string) => post<{ sent: boolean }>('/email', { email, locale });

export async function signOut(): Promise<void> {
  await post('/logout');
  remember(false);
}

export const setQuiet = (quiet: boolean) => post<{ member: Member }>('/quiet', { quiet });

export const fetchMine = () => call<{ member: Member; reports: MemberReport[] }>('/reports');

export const fetchQueue = (status = 'pending') => call<{ reports: Queued[] }>(`/review?${new URLSearchParams({ status })}`);

export const decide = (id: string, decision: 'publish' | 'reject', models: string[], note: string) =>
  post<{ points: number; total: number }>(`/review/${encodeURIComponent(id)}`, { decision, models, note });

/** The send form's answer: the report's receipt. */
export interface Sent { id: string; receipt_url: string; files: { kind: string; name: string; private?: boolean }[] }

export async function sendReport(form: FormData): Promise<Sent> {
  const r = await fetch(`${BASE}/reports`, { method: 'POST', body: form, credentials: 'same-origin' });
  let body: unknown = null;
  try { body = await r.json(); } catch { /* not JSON */ }
  if (!r.ok) throw new ClubError(r.status, (body as { error?: string } | null)?.error ?? `HTTP ${r.status}`);
  return body as Sent;
}

/** What one accepted thing earns, as the service counts it (reports.Points). */
export const STARS = { item: 1, dump: 10 } as const;

/** Seconds left until an ISO time, never below zero. */
export function secondsLeft(iso: string, now = Date.now()): number {
  return Math.max(0, Math.round((Date.parse(iso) - now) / 1000));
}

/** 9:41 */
export function clock(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
