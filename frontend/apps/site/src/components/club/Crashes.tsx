/**
 * A member's kernel crashes, on /club (service/internal/crashes): the crash
 * log their camera's WebUI downloaded, sent from here, and every crash they
 * sent or their linked cameras did, with the bug it was filed under.
 *
 * Stars are not shown per crash: the nightly settlement pays them, once per
 * camera and bug, and more to whoever reported a bug first once a maintainer
 * has confirmed it.
 */
import { useEffect, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { fetchMyCrashes, KIND_TONE, sendCrash, STATUS_TONE, type Mine, type Rules, type Sent } from '../../lib/crashes';

/** What the service pays (crashes.ReportStars ... MonthCap), until it says. */
const RULES: Rules = { report: 1, first: 3, fixed: 5, month_cap: 10 };

export default function Crashes({ locale, t, onChange }: { locale: Locale; t: BoardsT; onChange: () => void }) {
  const [data, setData] = useState<{ crashes: Mine[]; rules: Rules } | null>(null);
  const [error, setError] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [mac, setMac] = useState('');
  const [state, setState] = useState<{ s: 'idle' | 'sending' } | { s: 'sent'; r: Sent } | { s: 'error'; error: string }>({ s: 'idle' });

  // A club without crashes (an older service) answers nothing to read: the
  // section stays, empty.
  const load = () => fetchMyCrashes()
    .then((r) => setData({ crashes: Array.isArray(r?.crashes) ? r.crashes : [], rules: r?.rules ?? RULES }))
    .catch(() => setError(true));
  useEffect(() => { void load(); }, []);
  useEffect(() => {
    if (data && location.hash === '#crashes') document.getElementById('crashes')?.scrollIntoView();
  }, [data !== null]);

  const rules = data?.rules ?? RULES;

  const submit = (e: Event) => {
    e.preventDefault();
    if (!file) return;
    const form = new FormData();
    form.append('bundle', file);
    if (mac.trim()) form.append('mac', mac.trim());
    setState({ s: 'sending' });
    sendCrash(form)
      .then((r) => { setState({ s: 'sent', r }); setFile(null); void load(); onChange(); })
      .catch((err: Error) => setState({ s: 'error', error: err.message }));
  };

  return (
    <section id="crashes" class="grid gap-3 border-b border-hairline px-4 py-4" aria-labelledby="club-crashes">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="club-crashes" class="m-0 text-lg font-semibold">{t('crashes.mine_title')}</h2>
        <a class="text-[13px]" href={pathFor(locale, '/crashes')}>{t('crashes.all_link')}</a>
      </div>
      <p class="m-0 text-sm text-body-secondary">
        {t('crashes.mine_lede', { report: rules.report, first: rules.first, fixed: rules.fixed, cap: rules.month_cap })}
      </p>
      <form class="grid gap-2 rounded-lg border border-hairline p-3" onSubmit={submit}>
        <label for="crash-file" class="text-[13px] font-semibold">{t('crashes.file_label')}</label>
        <input id="crash-file" type="file" accept=".gz,.tgz,.tar,application/gzip,application/x-tar"
          onChange={(e) => { setFile((e.target as HTMLInputElement).files?.[0] ?? null); setState({ s: 'idle' }); }}
          class="text-sm" />
        <label for="crash-mac" class="text-[13px] text-body-secondary">{t('crashes.mac_label')}</label>
        <input id="crash-mac" value={mac} placeholder="aa:bb:cc:dd:ee:ff" autocomplete="off" spellcheck={false}
          onInput={(e) => setMac((e.target as HTMLInputElement).value)}
          class="max-w-[260px] rounded-md border border-hairline px-2 py-1 font-mono text-sm" />
        <button type="submit" class="site-btn site-btn-primary site-btn-sm w-fit disabled:opacity-55" disabled={!file || state.s === 'sending'}>
          {t('crashes.send')}
        </button>
        {state.s === 'sent' && (
          <p class="m-0 rounded-md bg-[#e7f5ee] px-3 py-2 text-sm text-[#1f7a4d]" role="status">
            {t(state.r.self_inflicted ? 'crashes.sent_self' : state.r.duplicate ? 'crashes.sent_again' : 'crashes.sent')}{' '}
            <a href={`${pathFor(locale, '/crashes')}#${state.r.signature}`} class="font-mono">{state.r.title}</a>
          </p>
        )}
        {state.s === 'error' && <p class="m-0 text-sm text-[#a3262e]" role="alert">{state.error}</p>}
      </form>
      {error && <p class="m-0 text-sm text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {data && data.crashes.length === 0 && <p class="m-0 text-sm text-body-secondary">{t('crashes.mine_empty')}</p>}
      {data && data.crashes.length > 0 && (
        <ul class="m-0 grid list-none gap-2 p-0">
          {data.crashes.map((c) => (
            <li key={c.id} class="grid gap-1 rounded-lg border border-hairline p-2.5 text-[13px]">
              <span class="flex flex-wrap items-center gap-2">
                <span class={`rounded-full px-2 py-0.5 text-[12px] font-semibold ${KIND_TONE[c.kind]}`}>{t(`crashes.kind_${c.kind}`)}</span>
                <span class={`rounded-full px-2 py-0.5 text-[12px] font-semibold ${STATUS_TONE[c.status]}`}>{t(`crashes.status_${c.status}`)}</span>
                {c.self_inflicted && <span class="text-[12px] text-body-secondary">{t('crashes.self_inflicted')}</span>}
                <span class="text-[12px] text-body-secondary">{c.received_at.slice(0, 10)} · {[c.soc, c.sensor].filter(Boolean).join(' · ').toUpperCase()}</span>
              </span>
              <a class="font-mono break-words" href={`${pathFor(locale, '/crashes')}#${c.signature}`}>{c.title}</a>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
