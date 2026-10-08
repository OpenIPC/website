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
  /** The stars' two sources: accepted reports, and cameras on the Open Wall. */
  report_stars?: number;
  wall_stars?: number;
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

/** A camera the catalogue does not have, as its sender named it. */
export interface Proposal { maker: string; board: string; soc?: string }

/** The board publishing a proposal adds (service boards.NewModel). */
export interface NewBoard { maker_id: string; maker_name: string; model_id: string; model: string; soc?: string }

/** What the review page offers for a proposal (boards.Suggestion). */
export interface Suggestion extends NewBoard {
  maker_known: boolean;
  /** A board that already answers to the marking: link to it instead. */
  existing?: string;
}

export interface MemberReport {
  id: string;
  received_at: string;
  status: ReportState;
  reviewed_at?: string;
  note?: string;
  review_note?: string;
  board?: BoardRef;
  proposal?: Proposal;
  /** The report this one was sent to go with, by a Club code. */
  joins?: string;
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
  proposal?: Proposal;
  new_board?: Suggestion;
  joins?: string;
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

/** The event the navbar's badge listens for: who is signed in now, or null. */
export const CHANGED = 'openipc-club';

function remember(member: Member | null) {
  try {
    if (member) localStorage.setItem(FLAG, '1');
    else localStorage.removeItem(FLAG);
  } catch { /* private mode */ }
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent<Member | null>(CHANGED, { detail: member }));
}

export async function fetchMe(): Promise<Me> {
  const me = await call<Me>('/me');
  remember(me.member);
  return me;
}

export const startTelegram = () => post<{ link: string; expires_at: string }>('/telegram');

export type Poll =
  | { state: 'none' | 'expired' }
  | { state: 'pending'; expires_at: string }
  | { state: 'signed_in'; member: Member };

export async function pollLogin(): Promise<Poll> {
  const p = await call<Poll>('/login');
  if (p.state === 'signed_in') remember(p.member);
  return p;
}

export const startEmail = (email: string, locale: string) => post<{ sent: boolean }>('/email', { email, locale });

export async function signOut(): Promise<void> {
  await post('/logout');
  remember(null);
}

export const setQuiet = (quiet: boolean) => post<{ member: Member }>('/quiet', { quiet });

export async function rename(name: string): Promise<Member> {
  const r = await post<{ member: Member }>('/name', { name });
  remember(r.member);
  return r.member;
}

/** Whose account a link opened in another browser signs into. */
export const finishWho = (code: string) =>
  call<{ provider: 'telegram' | 'email'; who: string }>(`/finish/who?${new URLSearchParams({ code })}`);

export async function finishConfirm(code: string): Promise<Member> {
  const r = await post<{ member: Member }>('/finish', { code });
  remember(r.member);
  return r.member;
}

export const fetchMine = () => call<{ member: Member; reports: MemberReport[] }>('/reports');

export const fetchQueue = (status = 'pending') => call<{ reports: Queued[] }>(`/review?${new URLSearchParams({ status })}`);

export const decide = (id: string, decision: 'publish' | 'reject', models: string[], note: string, newBoard?: NewBoard) =>
  post<{ points: number; total: number; board?: string }>(`/review/${encodeURIComponent(id)}`,
    newBoard ? { decision, models, note, new_board: newBoard } : { decision, models, note });

/** A code that makes `ipctool upload --note <code>` the member's report. */
export interface ReportCode { code: string; expires_at: string; joins?: string }

/** A code for a report sent from the camera, joining report joins if given. */
export const newReportCode = (joins?: string) =>
  post<{ code: ReportCode }>('/reports/code', joins ? { joins } : {}).then((r) => r.code);

/** The send form's answer: the report's receipt. */
export interface Sent { id: string; receipt_url: string; files: { kind: string; name: string; private?: boolean }[] }

export async function sendReport(form: FormData): Promise<Sent> {
  const r = await fetch(`${BASE}/reports`, { method: 'POST', body: form, credentials: 'same-origin' });
  let body: unknown = null;
  try { body = await r.json(); } catch { /* not JSON */ }
  if (!r.ok) throw new ClubError(r.status, (body as { error?: string } | null)?.error ?? `HTTP ${r.status}`);
  return body as Sent;
}

/**
 * What one accepted thing earns, as the service counts it: reports.Points
 * for reports, wallstars.JoinStars/MonthStars/RareStars for cameras.
 */
export const STARS = { item: 1, dump: 10, join: 5, month: 1, rare: 2 } as const;

/** A camera of the member's on the Open Wall (service/internal/wallstars). */
export interface Camera {
  token: string;
  name: string;
  soc: string;
  sensor: string;
  firmware: string;
  linked_at: string;
  first_seen: string;
  last_frame?: string;
  /** The last UTC day with a picture, YYYY-MM-DD. */
  last_day?: string;
  /** Qualifying days: a picture worth showing, and pictures that changed. */
  days: number;
  wall_days: number;
  stars: number;
  status: 'counting' | 'joined' | 'limit' | 'dark' | 'silent' | 'revoked';
  need_days: number;
  need_age: number;
  next_star: number;
  rare: boolean;
  show_owner: boolean;
}

export interface LinkCode { code: string; expires_at: string; blocked?: boolean }

export interface Cameras { cameras: Camera[]; code: LinkCode | null; listed: boolean; max_cameras: number }

export const fetchCameras = () => call<Cameras>('/cameras');

export const newCameraCode = () => post<{ code: LinkCode }>('/cameras/code');

export const unlinkCamera = (token: string) => post<{ unlinked: boolean }>(`/cameras/${encodeURIComponent(token)}/unlink`);

export const showOwner = (token: string, show: boolean) => post<{ show: boolean }>(`/cameras/${encodeURIComponent(token)}/owner`, { show });

export const setListed = (listed: boolean) => post<{ listed: boolean }>('/listed', { listed });

export interface Leader { rank: number; name: string; reports: number; wall: number; stars: number; you?: boolean }

export const fetchLeaderboard = (period: 'all' | '30d') =>
  call<{ period: string; members: Leader[] }>(`/leaderboard?${new URLSearchParams({ period })}`);

/** Seconds left until an ISO time, never below zero. */
export function secondsLeft(iso: string, now = Date.now()): number {
  return Math.max(0, Math.round((Date.parse(iso) - now) / 1000));
}

/** 9:41 */
export function clock(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
