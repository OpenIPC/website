/**
 * "What brought you here today?" (#185): when the strip asks, and what it
 * counts.
 *
 * An experiment for one quarter. Its answers are `why:<answer>:<locale>`
 * beacon events, which deploy/audience-memo.sh reads as H6; after ENDS the
 * strip renders nothing and the memo's section reads the quarter it had.
 * REMOVE AFTER 2027-01 with WhyStrip.tsx, the `why` strings in
 * data/locales/pages.*.yml and the memo's why: section, once the decision is
 * written on #185.
 *
 * The browser keeps one note under KEY and nothing else: `answered`, or the
 * day the strip was last shown or closed. No identifier, never sent anywhere.
 * /privacy says so (`records_why_html`, `row_browser_*`).
 */

export const KEY = 'openipc-why';

/** The last day is the one before; the strip stops asking on this date. */
export const ENDS = '2027-01-15';

/** In the order the strip offers them. The event takes the key, not the label. */
export const ANSWERS = ['flash', 'fpv', 'hardware', 'product', 'looking'] as const;
export type Answer = (typeof ANSWERS)[number];

/** How long a browser that closed the strip is left alone. */
export const QUIET_DAYS = 30;

/** The visitor's own calendar day, YYYY-MM-DD: "not twice a day" is their day. */
export function dayOf(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

function daysBetween(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000);
}

/**
 * Whether to ask, given the stored note and today's date.
 *
 * - `answered`: never again, so no one is counted twice.
 * - `shown:<day>`: not again that day, on either page. That is the "once a
 *   session" of the issue, and it holds across tabs, which sessionStorage
 *   would not.
 * - `dismissed:<day>`: not for QUIET_DAYS.
 * - anything unreadable: ask, and the note is rewritten when it shows.
 */
export function shouldAsk(stored: string | null, today: string): boolean {
  if (today >= ENDS) return false;
  if (stored === null) return true;
  if (stored === 'answered') return false;
  const m = /^(shown|dismissed):(\d{4}-\d{2}-\d{2})$/.exec(stored);
  if (!m) return true;
  const since = daysBetween(m[2], today);
  if (Number.isNaN(since)) return true;
  return m[1] === 'shown' ? since !== 0 : since >= QUIET_DAYS;
}

/** The beacon event: the answer and the locale it was given in (H4 reads zh). */
export function eventName(answer: Answer, locale: string): string {
  return `why:${answer}:${locale}`;
}

/** The strip's labels from the `pages.why` strings, for both pages that mount it. */
export function whyLabels(t: (key: string) => string) {
  const k = (s: string) => t(`pages.why.${s}`);
  return {
    question: k('question'),
    note: k('note'),
    how: k('how'),
    close: k('close'),
    thanks: k('thanks'),
    answers: Object.fromEntries(ANSWERS.map((a) => [a, k(a)])) as Record<Answer, string>,
  };
}
