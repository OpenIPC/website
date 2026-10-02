/**
 * /club/review: the maintainers' review queue. Each report shows who sent it,
 * the board they named or ipctool's guess, its files (private dumps
 * included, for the reviewer), and what publishing it would earn the sender.
 * Publishing links it to its boards, lists its text there, awards the stars
 * and tells the sender; rejecting takes back anything it had earned.
 *
 * The same decisions remain available as `openipc reports publish|reject`.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { ClubError, decide, fetchMe, fetchQueue, type Queued } from '../../lib/club';
import { size } from '../../lib/reports';
import { STATUS_TONE } from './parts';

type Load = { state: 'loading' } | { state: 'ok'; list: Queued[] } | { state: 'forbidden' } | { state: 'signed_out' } | { state: 'error' };
const TABS = ['pending', 'published', 'rejected'] as const;

export default function Review({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [tab, setTab] = useState<(typeof TABS)[number]>('pending');
  const [load, setLoad] = useState<Load>({ state: 'loading' });

  const reload = () => {
    setLoad({ state: 'loading' });
    fetchQueue(tab)
      .then((r) => setLoad({ state: 'ok', list: r.reports }))
      .catch((e) => {
        if (e instanceof ClubError && e.status === 401) setLoad({ state: 'signed_out' });
        else if (e instanceof ClubError && e.status === 403) setLoad({ state: 'forbidden' });
        else setLoad({ state: 'error' });
      });
  };
  useEffect(() => { void fetchMe().catch(() => {}); }, []);
  useEffect(reload, [tab]);

  if (load.state === 'signed_out' || load.state === 'forbidden') {
    return (
      <p class="mt-8 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">
        {t('club.review_forbidden')} <a href={pathFor(locale, '/club')}>{t('club.sign_in_link')}</a>
      </p>
    );
  }
  return (
    <div class="mt-8 grid gap-4">
      <div class="flex flex-wrap gap-2" role="tablist">
        {TABS.map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}
            class={`cursor-pointer rounded-md border px-3 py-1 text-sm ${tab === k ? 'border-brand-blue bg-[#eef0fc] font-semibold text-[#3d4dad]' : 'border-hairline bg-white'}`}>
            {t(`club.tab_${k}`)}
          </button>
        ))}
      </div>
      {load.state === 'loading' && <div class="h-40 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />}
      {load.state === 'error' && <p class="m-0 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {load.state === 'ok' && load.list.length === 0 && <p class="m-0 text-body-secondary">{t('club.review_empty')}</p>}
      {load.state === 'ok' && load.list.map((q) => <Item key={q.id} q={q} locale={locale} t={t} onDone={reload} />)}
    </div>
  );
}

function Item({ q, locale, t, onDone }: { q: Queued; locale: Locale; t: BoardsT; onDone: () => void }) {
  const [models, setModels] = useState(q.models.join(', '));
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const received = new Date(q.received_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });

  const act = (decision: 'publish' | 'reject') => {
    setBusy(true);
    setError(null);
    const ids = models.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
    decide(q.id, decision, ids, note)
      .then((r) => { setResult(t(`club.review_done_${decision}`, { points: r.points })); setTimeout(onDone, 1200); })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  };

  return (
    <article class="grid gap-3 rounded-xl border border-hairline p-4" aria-labelledby={`q-${q.id}`}>
      <header class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id={`q-${q.id}`} class="m-0 text-base font-semibold">
          {q.board ? `${q.board.manufacturer} ${q.board.model}` : (q.chip || q.id)}
          <span class="ms-2 font-mono text-[12px] font-normal text-body-secondary">{q.id} · {q.channel}</span>
        </h2>
        <span class="flex items-center gap-2 text-[13px] text-body-secondary">
          {received}
          <span class={`rounded-full px-2.5 py-0.5 text-[12.5px] font-semibold ${STATUS_TONE[q.status]}`}>{t(`club.status_${q.status}`)}</span>
        </span>
      </header>
      <dl class="m-0 grid grid-cols-1 gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_1fr]">
        <dt class="text-body-secondary">{t('club.col_submission')}</dt>
        <dd class="m-0">{q.member ? t('club.review_from', { who: q.member }) : t('club.review_anon')}</dd>
        {q.board && <><dt class="text-body-secondary">{t('club.review_board')}</dt>
          <dd class="m-0"><a href={`${pathFor(locale, '/cameras/boards')}?model=${encodeURIComponent(q.board.id)}`} class="font-mono">{q.board.id}</a></dd></>}
        {q.guess && <><dt class="text-body-secondary">ipctool</dt><dd class="m-0">{t('club.review_guess', { board: `${q.guess.manufacturer} ${q.guess.model}` })} <span class="font-mono text-[12px]">{q.guess.model_id}</span></dd></>}
        {(q.chip || q.sensor) && <><dt class="text-body-secondary">SoC</dt><dd class="m-0">{[q.chip, q.sensor].filter(Boolean).join(' · ')}</dd></>}
        {q.note && <><dt class="text-body-secondary">{t('club.kind_note')}</dt><dd class="m-0 whitespace-pre-wrap">{q.note}</dd></>}
      </dl>
      <ul class="m-0 grid list-none gap-1 p-0 text-sm">
        {q.file_list.map((f) => (
          <li key={f.position} class="flex flex-wrap items-center gap-x-2">
            <a href={f.url} download={f.name}>{t(`club.kind_${f.kind}`)} · {f.name}</a>
            <span class="text-[12.5px] text-body-secondary tabular-nums">{size(f.bytes)}</span>
            {f.private && <span class="rounded-full bg-[#eef0fc] px-2 text-[11.5px] font-semibold text-[#3d4dad]">{t('club.private')}</span>}
            <span class="text-[12.5px] font-semibold text-[#9a5b00]">{f.points > 0 ? `+${f.points} ★` : f.kind === 'backup' ? t('club.duplicate') : ''}</span>
          </li>
        ))}
      </ul>
      {q.yaml && (
        <details class="text-sm">
          <summary class="cursor-pointer text-brand-blue">{t('club.show_yaml')}</summary>
          <pre class="mt-2 max-h-72 overflow-auto rounded-md bg-surface-alt p-3 font-mono text-[12.5px]">{q.yaml}</pre>
        </details>
      )}
      <div class="grid gap-2 border-t border-hairline pt-3">
            <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_models')}
              <input value={models} onInput={(e) => setModels((e.target as HTMLInputElement).value)} placeholder={q.board?.id ?? ''}
                class="rounded-md border border-hairline px-2.5 py-1.5 font-mono text-sm text-body" />
            </label>
            <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_note')}
              <input value={note} onInput={(e) => setNote((e.target as HTMLInputElement).value)}
                class="rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body" />
            </label>
            <div class="flex flex-wrap items-center gap-2">
              {q.status !== 'published' && (
                <button type="button" disabled={busy} class="site-btn site-btn-primary" onClick={() => act('publish')}>
                  {t('club.publish')}{q.potential > 0 && ` · +${q.potential} ★`}
                </button>
              )}
              {q.status !== 'rejected' && <button type="button" disabled={busy} class="site-btn site-btn-outline-secondary" onClick={() => act('reject')}>{t('club.reject')}</button>}
              {result && <span class="text-sm text-[#146c3c]" role="status">{result}</span>}
              {error && <span class="text-sm text-[#a3262e]" role="alert">{error}</span>}
            </div>
      </div>
    </article>
  );
}
