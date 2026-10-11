import { useEffect, useState } from 'preact/hooks';
import { ANSWERS, KEY, dayOf, eventName, shouldAsk, type Answer } from '../lib/why';

/**
 * "What brought you here today?" (#185), under the hero of / and /get-started.
 *
 * One tap counts `why:<answer>:<locale>` and says thank you; closing it counts
 * nothing. It is in the page's flow and styled as the site's quiet chips, not
 * fixed and not overlaid: a strip that reads as a cookie banner would undo
 * #182, and would be dismissed as one.
 *
 * Renders nothing on the server and until it has read the browser's note, so
 * a browser that has answered never sees it flash. Where storage is refused
 * (a private window) it does not ask at all: it could not keep the promise
 * of asking once. lib/why.ts has the rules and the end date.
 */
interface Labels {
  question: string;
  note: string;
  how: string;
  close: string;
  thanks: string;
  answers: Record<Answer, string>;
}

declare global {
  interface Window { openipcCount?: (name: string) => void }
}

function remember(note: string): boolean {
  try { localStorage.setItem(KEY, note); return true; } catch { return false; }
}

export default function WhyStrip({ locale, labels, privacyHref, class: className = '' }: {
  locale: string;
  labels: Labels;
  privacyHref: string;
  class?: string;
}) {
  const [state, setState] = useState<'hidden' | 'asking' | 'thanked'>('hidden');

  useEffect(() => {
    const today = dayOf(new Date());
    let stored: string | null;
    try { stored = localStorage.getItem(KEY); } catch { return; }
    // Written before it shows: a note that cannot be kept means no strip.
    if (shouldAsk(stored, today) && remember(`shown:${today}`)) setState('asking');
  }, []);

  if (state === 'hidden') return null;

  const answer = (a: Answer) => {
    // The sender is inlined into every page's head (lib/beacon.inline.js);
    // the guard is for a page someone saved to disk.
    window.openipcCount?.(eventName(a, locale));
    remember('answered');
    setState('thanked');
  };

  const dismiss = () => {
    remember(`dismissed:${dayOf(new Date())}`);
    setState('hidden');
  };

  return (
    <section class={`border-b border-hairline bg-white ${className}`} aria-labelledby="why-question">
      <div class="site-container">
        {state === 'thanked' ? (
          <p class="mb-0 py-3.5 text-body-secondary" role="status">
            <span class="mr-2.5 inline-block size-2 -translate-y-px rounded-full bg-accent" aria-hidden="true"></span>
            {labels.thanks}
          </p>
        ) : (
          <div class="relative flex flex-wrap items-center gap-x-3.5 gap-y-2.5 py-3.5 pr-9">
            <p id="why-question" class="mb-0 basis-full font-semibold text-ink sm:basis-auto">{labels.question}</p>
            <ul class="m-0 flex list-none flex-wrap gap-2 p-0">
              {ANSWERS.map((a) => (
                <li key={a}>
                  <button
                    type="button"
                    class="cursor-pointer rounded-full border border-hairline bg-white px-3 py-1.5 text-sm leading-tight text-body hover:border-brand-blue hover:text-brand-blue focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-blue"
                    onClick={() => answer(a)}
                  >
                    {labels.answers[a]}
                  </button>
                </li>
              ))}
            </ul>
            <p class="mb-0 basis-full text-[0.8rem] text-body-secondary">
              {labels.note} <a href={privacyHref}>{labels.how}</a>
            </p>
            <button
              type="button"
              class="absolute top-2.5 right-0 size-[30px] cursor-pointer rounded-md text-xl leading-none text-body-secondary hover:bg-surface-alt hover:text-ink focus-visible:outline-2 focus-visible:outline-brand-blue"
              aria-label={labels.close}
              onClick={dismiss}
            >
              ×
            </button>
          </div>
        )}
      </div>
    </section>
  );
}
