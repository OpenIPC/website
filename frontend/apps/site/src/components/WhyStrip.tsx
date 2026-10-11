import { useEffect, useRef, useState } from 'preact/hooks';
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
 * a browser that has answered never sees it flash. The day's showing is noted
 * once the strip is on screen, and an answer counts only once the note that
 * stops the next question is kept. Where storage is refused (a private
 * window) it does not ask at all: it could not keep the promise of asking
 * once. lib/why.ts has the rules and the end date.
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

/** The note now, or `undefined` where storage cannot be read. */
function note(): string | null | undefined {
  try { return localStorage.getItem(KEY); } catch { return undefined; }
}

export default function WhyStrip({ locale, labels, privacyHref, class: className = '' }: {
  locale: string;
  labels: Labels;
  privacyHref: string;
  class?: string;
}) {
  const [state, setState] = useState<'hidden' | 'asking' | 'thanked'>('hidden');
  const box = useRef<HTMLElement>(null);

  useEffect(() => {
    const stored = note();
    if (stored !== undefined && shouldAsk(stored, dayOf(new Date()))) setState('asking');
  }, []);

  // A showing counts when the strip is SEEN, not when it hydrates: on the
  // home page it follows a tall hero, and a visitor who goes straight to
  // /get-started from the top of it has not been asked yet. Noting it fails
  // where storage is full or refused, and then it does not ask.
  useEffect(() => {
    const el = box.current;
    if (state !== 'asking' || !el) return;
    const seen = () => { if (!remember(`shown:${dayOf(new Date())}`)) setState('hidden'); };
    if (typeof IntersectionObserver === 'undefined') { seen(); return; }
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) { io.disconnect(); seen(); }
    });
    io.observe(el);
    return () => io.disconnect();
  }, [state]);

  // Answered or closed in another tab: this one stops asking too.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === KEY && (e.newValue === 'answered' || e.newValue?.startsWith('dismissed:'))) {
        setState((s) => (s === 'asking' ? 'hidden' : s));
      }
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  if (state === 'hidden') return null;

  const answer = (a: Answer) => {
    // Once per browser: not if another tab has answered since this one asked,
    // and not unless the note that stops the next question is kept -- an
    // answer that cannot be remembered is not counted.
    if (note() === 'answered' || !remember('answered')) { setState('hidden'); return; }
    // The sender is inlined into every page's head (lib/beacon.inline.js);
    // the guard is for a page someone saved to disk.
    window.openipcCount?.(eventName(a, locale));
    setState('thanked');
  };

  const dismiss = () => {
    remember(`dismissed:${dayOf(new Date())}`);
    setState('hidden');
  };

  return (
    <section
      ref={box}
      class={`border-b border-hairline bg-white ${className}`}
      aria-labelledby={state === 'asking' ? 'why-question' : undefined}
    >
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
