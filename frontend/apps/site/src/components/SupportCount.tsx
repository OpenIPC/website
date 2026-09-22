import { useEffect, useState } from 'preact/hooks';
import {
  STATS_URL, goalMet, monthlyUsd, parseStats, progressPercent, type SupportStats,
} from '../lib/support';

/**
 * The backer count, filled in on load (#160).
 *
 * Renders nothing at all until it has a usable figure, and nothing ever if it
 * does not get one -- which is exactly what shared/_support_count does, and
 * leaves the page as it was before the count existed. That is the safety
 * property, not a fallback: a stale or absent number must never become a claim.
 *
 * Three placements share it in Rails -- the donate page above the button, the
 * wizard's success block, and the note under the home CTA band -- so the
 * number the site quotes is one number and changing the sentence changes it
 * everywhere. Two of those are static pages now; the wizard is still Rails.
 */
interface Labels {
  /** `%{backers}` and `%{goal}`. */
  count: string;
  /** `%{backers}`, for when the goal is already met. */
  countMet: string;
  /** `%{backers}` and `%{goal}`. */
  meter: string;
  /** `%{amount}`. */
  monthly: string;
  /** `%{oc}` and `%{paywall}`. */
  split: string;
}

function fill(template: string, vars: Record<string, string | number>): string {
  return template.replace(/%\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  );
}

export default function SupportCount({ goal, labels, class: className = '' }: {
  goal: number;
  labels: Labels;
  class?: string;
}) {
  const [stats, setStats] = useState<SupportStats | null>(null);

  useEffect(() => {
    let live = true;
    fetch(STATS_URL, { headers: { accept: 'application/json' } })
      .then((response) => (response.ok ? response.json() : null))
      .then((raw) => { if (live) setStats(parseStats(raw)); })
      // A 404 is the documented state before the file exists and between the
      // write and the rename, and the page is correct without the number, so
      // there is nothing to report and nothing to retry.
      .catch(() => {});
    return () => { live = false; };
  }, []);

  if (!stats) return null;

  const percent = progressPercent(stats, goal);
  const sentence = goalMet(stats, goal)
    ? fill(labels.countMet, { backers: stats.backers })
    : fill(labels.count, { backers: stats.backers, goal });

  return (
    <div class={className}>
      {/*
        Two sentences, because "help us reach 75" printed beside a 75 is not an
        ask, it is a mistake the reader notices before we do.
      */}
      <p class="mb-2" dangerouslySetInnerHTML={{ __html: sentence }} />

      {/*
        A meter, not a progress bar: this measures people against a target
        somebody chose, and `progress` would announce it to a screen reader as
        a task completing. The fill is the accent on a lighter step of the same
        hue -- never green or amber, which would say "healthy" or "warning"
        about a number that is neither.
      */}
      <div
        class="h-2 w-full overflow-hidden rounded-full bg-brand-blue/15"
        role="meter"
        aria-valuenow={stats.backers}
        aria-valuemin={0}
        aria-valuemax={goal}
        aria-label={fill(labels.meter, { backers: stats.backers, goal })}
      >
        <span class="block h-full rounded-full bg-accent" style={`width: ${percent}%`} />
      </div>

      <p class="mt-2 mb-0 text-sm text-body-secondary">
        <span dangerouslySetInnerHTML={{ __html: fill(labels.monthly, { amount: `$${monthlyUsd(stats)}` }) }} />
        {/*
          Where the people are, when both channels are counted (#201). Said
          plainly rather than folded into one figure: a Russian reader seeing
          only a dollar total would reasonably conclude PayWall is not counted.
        */}
        {stats.paywallSubscribers !== null && (
          <>
            {' '}
            <span dangerouslySetInnerHTML={{
              __html: fill(labels.split, { oc: stats.backersOc ?? 0, paywall: stats.paywallSubscribers }),
            }} />
          </>
        )}
      </p>
    </div>
  );
}
