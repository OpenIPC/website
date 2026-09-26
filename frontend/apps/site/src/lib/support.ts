/**
 * How many people pay for OpenIPC every month, read in the browser (#160).
 *
 * The backer count's rules, applied in the browser to a file an hourly cron
 * writes. A prerendered page cannot: the count would be baked into a bundle
 * that may sit behind a cache for far longer than the number is good for, and
 * SupportStats exists precisely to stop a page outliving the figure printed on
 * it. So the page ships without it and fills it in on load.
 *
 * nginx already serves the same file at /api/v1/support/stats.json -- same
 * origin, CORS-open, cached an hour, and 404 when it is missing or mid-rename.
 *
 * `usable` below is SupportStats#usable? and #halves_agree?, reimplemented
 * rather than approximated, because the rules are the safety property:
 *
 *   * a count older than 48 hours is refused. A stale firmware index still
 *     answers every download it lists; a stale backer count is a false claim
 *     about people, printed next to a request for money.
 *   * oc-stats.sh writes the two channels and their sum, so if the halves are
 *     present and do not add up, the file is not the file this thinks it is
 *     reading -- someone edited it on the host, or the writer changed shape --
 *     and the whole thing is refused rather than half of it believed.
 *
 * When anything is wrong the page renders as it did before the count existed,
 * which is what shared/_support_count does and says in its own comment.
 */
export const STALE_AFTER_MS = 48 * 60 * 60 * 1000;

export interface SupportStats {
  backers: number;
  backersOc: number | null;
  paywallSubscribers: number | null;
  monthlyCents: number;
  fetchedAt: Date;
}

function wholeNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0;
}

/**
 * Parse and validate, or give nothing back. `null`, `[1,2]` and `42` are all
 * valid JSON and none of them is this file.
 */
export function parseStats(raw: unknown, now: Date = new Date()): SupportStats | null {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return null;
  const data = raw as Record<string, unknown>;

  const backers = data.backers;
  const monthlyCents = data.monthly_cents;
  const fetchedAtRaw = data.fetched_at;

  if (!wholeNumber(backers) || backers <= 0) return null;
  if (!wholeNumber(monthlyCents)) return null;
  if (typeof fetchedAtRaw !== 'string') return null;

  const fetchedAt = new Date(fetchedAtRaw);
  if (Number.isNaN(fetchedAt.getTime())) return null;
  if (now.getTime() - fetchedAt.getTime() > STALE_AFTER_MS) return null;

  // halves_agree?: both absent is fine, one absent is not, and present ones
  // must sum to the total.
  const oc = data.backers_oc ?? null;
  const paywall = data.paywall_subscribers ?? null;
  const halves = [oc, paywall];
  if (!halves.every((h) => h === null)) {
    if (!halves.every(wholeNumber)) return null;
    if ((oc as number) + (paywall as number) !== backers) return null;
  }

  return {
    backers,
    backersOc: oc as number | null,
    paywallSubscribers: paywall as number | null,
    monthlyCents,
    fetchedAt,
  };
}

/**
 * Rounded, not floored. The page prints whole dollars, and integer division
 * turned 199 cents into $1 -- understating what people give, on the page
 * thanking them for it.
 */
export function monthlyUsd(stats: SupportStats): number {
  return Math.round(stats.monthlyCents / 100);
}

/**
 * Capped at the goal so the meter cannot overflow its track. Passing the goal
 * is a good problem and shows as a full bar until someone raises it in
 * data/support_goal.yml -- which is a pull request, deliberately, so the
 * number does not move on its own.
 */
export function progressPercent(stats: SupportStats, goal: number): number {
  if (stats.backers >= goal) return 100;
  return Math.round((stats.backers / goal) * 100);
}

export function goalMet(stats: SupportStats, goal: number): boolean {
  return stats.backers >= goal;
}

export const STATS_URL = '/api/v1/support/stats.json';
