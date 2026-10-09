/**
 * /club/crashes: the maintainers' triage of kernel crashes and majestic's.
 * Every signature, worst first; one opens to what it takes to reproduce it --
 * the chips, sensors and builds it was seen on, the firmware builds whose time
 * matches its kernel, and each crash's redacted log, warnings and lead-up, or
 * for majestic's, its backtrace from the debuginfo of its build.
 *
 * /club/crashes/#<signature> opens that signature: the link a maintainer
 * shares, and the one a sent crash answers with.
 *
 * Confirming a bug pays its first reporter, fixing it pays them again;
 * bogus takes back every star it paid. The same is `openipc crashes status`.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { ClubError, fetchMe } from '../../lib/club';
import {
  decideCrash, fetchCrashDetail, fetchTriage, frame, frameAt, KIND_TONE, STATUS_TONE, type Combo, type Detail, type Frame, type Signature,
  type Status,
} from '../../lib/crashes';

type Load = { state: 'loading' } | { state: 'ok'; list: Signature[] } | { state: 'forbidden' } | { state: 'error' };
const STATUSES: Status[] = ['open', 'confirmed', 'fixed', 'wontfix', 'bogus'];

export default function CrashTriage({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [open, setOpen] = useState<string | null>(null);

  const reload = () => fetchTriage()
    .then((r) => setLoad({ state: 'ok', list: r.signatures }))
    .catch((e) => setLoad(e instanceof ClubError && (e.status === 401 || e.status === 403) ? { state: 'forbidden' } : { state: 'error' }));
  useEffect(() => { void fetchMe().catch(() => {}); void reload(); }, []);

  // The signature the address names: on arrival, and when the hash changes
  // under an open page (a second link followed in the same tab).
  useEffect(() => {
    const follow = () => {
      const hash = location.hash.slice(1);
      if (/^[0-9a-f]{12}$/.test(hash)) setOpen(hash);
    };
    follow();
    addEventListener('hashchange', follow);
    return () => removeEventListener('hashchange', follow);
  }, []);
  const listed = load.state === 'ok';
  useEffect(() => {
    if (open && listed) document.getElementById(open)?.scrollIntoView({ block: 'start' });
  }, [open, listed]);
  const toggle = (id: string) => {
    const next = open === id ? null : id;
    setOpen(next);
    history.replaceState(null, '', next ? `#${next}` : location.pathname);
  };

  if (load.state === 'forbidden') {
    return (
      <p class="mt-8 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">
        {t('crashes.triage_forbidden')} <a href={pathFor(locale, '/club')}>{t('club.sign_in_link')}</a>
      </p>
    );
  }
  return (
    <div class="mt-8 grid gap-3">
      {load.state === 'loading' && <div class="h-40 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />}
      {load.state === 'error' && <p class="m-0 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {load.state === 'ok' && load.list.length === 0 && <p class="m-0 text-body-secondary">{t('crashes.empty')}</p>}
      {load.state === 'ok' && load.list.map((g) => (
        <article key={g.id} id={g.id} class={`grid gap-2 rounded-lg border p-3 ${g.merged_into || g.status === 'bogus' ? 'opacity-60' : ''} ${open === g.id ? 'border-brand-blue' : 'border-hairline'}`}>
          <button type="button" class="grid cursor-pointer gap-1 bg-transparent p-0 text-left" aria-expanded={open === g.id}
            onClick={() => toggle(g.id)}>
            <span class="flex flex-wrap items-center gap-2 text-[12px]">
              <span class="font-mono font-semibold tabular-nums">{g.score}</span>
              <span class={`rounded-full px-2 py-0.5 font-semibold ${KIND_TONE[g.kind]}`}>{t(`crashes.kind_${g.kind}`)}{g.in_irq ? ` · ${t('crashes.in_irq')}` : ''}</span>
              <span class={`rounded-full px-2 py-0.5 font-semibold ${STATUS_TONE[g.status]}`}>{t(`crashes.status_${g.status}`)}</span>
              {g.merged_into && <span class="text-body-secondary">{t('crashes.merged_into', { id: g.merged_into })}</span>}
              <span class="text-body-secondary">{t('crashes.cameras', { count: g.cameras })} · {t('crashes.events', { count: g.events })} · {[...g.socs, ...g.sensors].join(' ').toUpperCase()}</span>
            </span>
            <span class="font-mono text-[14px] font-semibold break-words text-ink">{g.title}</span>
          </button>
          {open === g.id && <Open g={g} t={t} onDone={() => { void reload(); }} />}
        </article>
      ))}
    </div>
  );
}

function Open({ g, t, onDone }: { g: Signature; t: BoardsT; onDone: () => void }) {
  const [data, setData] = useState<{ seen_on: Combo[]; crashes: Detail[] } | null>(null);
  const [status, setStatus] = useState<Status>(g.status);
  const [fixedIn, setFixedIn] = useState(g.fixed_in ?? '');
  const [issue, setIssue] = useState(g.issue_url ?? '');
  // Saving sends every field: what is shown is kept, an emptied field is cleared.
  const [merge, setMerge] = useState(g.merged_into ?? '');
  const [note, setNote] = useState(g.note ?? '');
  const [msg, setMsg] = useState<string | null>(null);
  useEffect(() => { fetchCrashDetail(g.id).then(setData).catch((e: Error) => setMsg(e.message)); }, [g.id]);

  const save = (e: Event) => {
    e.preventDefault();
    setMsg(null);
    decideCrash(g.id, { status, fixed_in: fixedIn, issue_url: issue, merge_into: merge, note })
      .then((r) => { setMsg(r.stars_taken_back ? t('crashes.taken_back', { n: r.stars_taken_back }) : t('crashes.saved')); onDone(); })
      .catch((err: Error) => setMsg(err.message));
  };
  const field = 'min-w-0 rounded-md border border-hairline px-2 py-1 text-sm';
  return (
    <div class="grid gap-3 border-t border-hairline pt-3 text-[13px]">
      <Backtrace frames={g.frames.slice(0, 10)} t={t} />
      <form class="grid gap-2 sm:grid-cols-2" onSubmit={save}>
        <label class="grid gap-1">{t('crashes.f_status')}
          <select class={field} value={status} onChange={(e) => setStatus((e.target as HTMLSelectElement).value as Status)}>
            {STATUSES.map((s) => <option key={s} value={s}>{t(`crashes.status_${s}`)}</option>)}
          </select>
        </label>
        <label class="grid gap-1">{t('crashes.f_fixed_in')}<input class={field} value={fixedIn} onInput={(e) => setFixedIn((e.target as HTMLInputElement).value)} /></label>
        <label class="grid gap-1">{t('crashes.f_issue')}<input class={field} type="url" placeholder="https://github.com/OpenIPC/firmware/issues/…" value={issue} onInput={(e) => setIssue((e.target as HTMLInputElement).value)} /></label>
        <label class="grid gap-1">{t('crashes.f_merge')}<input class={`${field} font-mono`} placeholder="0123456789ab" value={merge} onInput={(e) => setMerge((e.target as HTMLInputElement).value)} /></label>
        <label class="grid gap-1 sm:col-span-2">{t('crashes.f_note')}<textarea class={field} rows={2} value={note} onInput={(e) => setNote((e.target as HTMLTextAreaElement).value)} /></label>
        <div class="flex items-center gap-3 sm:col-span-2">
          <button type="submit" class="site-btn site-btn-primary site-btn-sm">{t('crashes.f_save')}</button>
          {msg && <span role="status">{msg}</span>}
        </div>
      </form>
      {data && (
        <>
          <table class="w-full border-collapse">
            <thead>
              <tr class="text-left text-[12px] text-body-secondary">
                <th class="py-1 font-medium">{t('crashes.col_soc')}</th><th class="py-1 font-medium">{t('crashes.col_sensor')}</th>
                <th class="py-1 font-medium">{t('crashes.col_build')}</th><th class="py-1 text-right font-medium">{t('crashes.col_cameras')}</th>
              </tr>
            </thead>
            <tbody>
              {data.seen_on.map((c, i) => (
                <tr key={i} class="border-t border-hairline font-mono text-[12px]">
                  <td class="py-1">{c.soc}</td><td class="py-1">{c.sensor}</td>
                  <td class="py-1">{[c.kernel, c.kernel_built?.slice(0, 16), c.firmware, c.majestic].filter(Boolean).join(' · ')}</td>
                  <td class="py-1 text-right">{c.cameras}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {data.crashes.map((d) => <Crash key={d.id} d={d} t={t} />)}
        </>
      )}
    </div>
  );
}

/** A backtrace, innermost first; the scan's probable callers marked so. */
function Backtrace({ frames, t }: { frames: Frame[]; t: BoardsT }) {
  return (
    <pre class="m-0 overflow-x-auto rounded-md bg-surface-alt p-2.5 font-mono text-[12px] leading-relaxed">
      {frames.map((f, i) => `${i === 0 ? '' : '← '}${frameAt(f)}${f.probable ? `  (${t('crashes.probable')})` : ''}`).join('\n')}
    </pre>
  );
}

function Crash({ d, t }: { d: Detail; t: BoardsT }) {
  const anomalies = Object.entries(d.anomalies ?? {});
  return (
    <details class="rounded-md border border-hairline p-2.5">
      <summary class="cursor-pointer">
        <span class="font-mono">{d.id}</span> · {d.received_at.slice(0, 16).replace('T', ' ')} · {d.channel}
        {d.self_inflicted && ` · ${t('crashes.self_inflicted')}`}
      </summary>
      <dl class="m-0 mt-2 grid gap-x-3 gap-y-0.5 sm:grid-cols-[max-content_1fr]">
        <dt class="text-body-secondary">{t('crashes.d_hardware')}</dt><dd class="m-0 font-mono">{[d.soc, d.sensor, d.board, d.machine].filter(Boolean).join(' · ')}</dd>
        <dt class="text-body-secondary">{t('crashes.d_firmware')}</dt><dd class="m-0 font-mono">{[d.firmware, d.majestic && `majestic ${d.majestic}`].filter(Boolean).join(' · ') || '?'}</dd>
        {d.kernel && <><dt class="text-body-secondary">{t('crashes.d_kernel')}</dt><dd class="m-0 font-mono">{d.kernel} {d.kernel_build}</dd></>}
        {d.builds && d.builds.length > 0 && (
          <><dt class="text-body-secondary">{t('crashes.d_builds')}</dt><dd class="m-0 font-mono">{d.builds.map((b) => `${b.release} (${b.sha.slice(0, 7)})`).join(', ')}</dd></>
        )}
        <dt class="text-body-secondary">{t('crashes.d_task')}</dt><dd class="m-0 font-mono">{d.fatal.comm ?? '?'}{d.fatal.interrupted?.length ? ` · ${t('crashes.d_interrupted')} ${d.fatal.interrupted.map(frame).join(' ← ')}` : ''}</dd>
        {d.uptime != null && <><dt class="text-body-secondary">{t('crashes.d_uptime')}</dt><dd class="m-0 tabular-nums">{Math.round(d.uptime)} s</dd></>}
        {d.cmdline && <><dt class="text-body-secondary">{t('crashes.d_cmdline')}</dt><dd class="m-0 font-mono break-all">{d.cmdline}</dd></>}
        {anomalies.length > 0 && <><dt class="text-body-secondary">{t('crashes.d_anomalies')}</dt><dd class="m-0 font-mono">{anomalies.map(([k, v]) => `${k} ×${v}`).join(', ')}</dd></>}
        {d.before.length > 0 && <><dt class="text-body-secondary">{t('crashes.d_before')}</dt><dd class="m-0 font-mono">{d.before.map((b) => `${b.reason} (${b.comm ?? '?'}) ${b.frames.slice(0, 3).map(frame).join(' ← ')}`).join('; ')}</dd></>}
      </dl>
      {d.symbolization && <Symbolized s={d.symbolization} t={t} />}
      {d.leadup.length > 0 && <pre class="mt-2 mb-0 overflow-x-auto rounded bg-surface-alt p-2 font-mono text-[11.5px]">{d.leadup.join('\n')}</pre>}
      {d.meta != null && <pre class="mt-2 mb-0 max-h-60 overflow-auto rounded bg-surface-alt p-2 font-mono text-[11.5px]">{JSON.stringify(d.meta, null, 2)}</pre>}
      {d.log && <pre class="mt-2 mb-0 max-h-96 overflow-auto rounded bg-ink p-2 font-mono text-[11.5px] text-white">{d.log}</pre>}
    </details>
  );
}

/** How a majestic crash's dump was unwound: its backtrace, or why not yet. */
function Symbolized({ s, t }: { s: NonNullable<Detail['symbolization']>; t: BoardsT }) {
  return (
    <div class="mt-2 grid gap-1">
      <span class="text-body-secondary">
        {t('crashes.d_backtrace')}
        {s.status === 'pending' && ` · ${t('crashes.sym_pending', { n: s.attempts })}`}
        {s.status === 'failed' && ` · ${t('crashes.sym_failed')}`}
        {s.error && <span class="font-mono"> · {s.error}</span>}
      </span>
      {s.frames.length > 0 && <Backtrace frames={s.frames} t={t} />}
    </div>
  );
}
