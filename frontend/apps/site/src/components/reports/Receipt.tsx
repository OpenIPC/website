/**
 * A report's receipt, at /cameras/report/?id=r-xxxxxxxx: the address ipctool
 * prints. It shows what arrived and who may see it; the report's content
 * appears only once a maintainer has published it, and then on the board.
 * With no ?id= it is the lookup form.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { NotFound, RECEIPT_ID, fetchReport, size, summary, type Report } from '../../lib/reports';

type Load = { state: 'none' } | { state: 'loading' } | { state: 'ok'; value: Report } | { state: 'missing' } | { state: 'error' };

const PILL = 'inline-flex items-center gap-1.5 rounded-full px-3 py-0.5 text-[13px] font-semibold';
const TONE: Record<string, string> = {
  pending: 'bg-[#fff4e0] text-[#8a5a00]',
  published: 'bg-[#e7f5ee] text-[#1f7a4d]',
  rejected: 'bg-surface-alt text-body-secondary',
  withdrawn: 'bg-surface-alt text-body-secondary',
};

export default function Receipt({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [id, setId] = useState('');
  const [load, setLoad] = useState<Load>({ state: 'none' });

  useEffect(() => {
    const q = new URLSearchParams(location.search).get('id')?.trim().toLowerCase() ?? '';
    setId(q);
    if (!RECEIPT_ID.test(q)) return;
    setLoad({ state: 'loading' });
    fetchReport(q)
      .then((value) => setLoad({ state: 'ok', value }))
      .catch((e) => setLoad({ state: e instanceof NotFound ? 'missing' : 'error' }));
  }, []);

  const form = (
    <form class="mt-6 flex flex-wrap items-center gap-2.5 text-[.9375rem]" method="get">
      <label for="receipt-id">{t('report.lookup_label')}</label>
      <input id="receipt-id" name="id" value={id} spellcheck={false} autocomplete="off" placeholder="r-xxxxxxxx"
        class="w-[14ch] rounded-md border border-[#cdd3e0] px-3 py-1.5 font-mono text-[15px]" />
      <button type="submit" class="site-btn site-btn-dark">{t('report.lookup_button')}</button>
    </form>
  );

  if (load.state === 'none') return form;
  if (load.state === 'loading') return <div class="mt-6 h-40 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />;
  if (load.state === 'missing' || load.state === 'error') {
    return (
      <div class="mt-6">
        <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-sm text-[#9a5b00]" role="alert">
          {t(load.state === 'missing' ? 'report.not_found' : 'report.load_failed')}
        </p>
        {form}
      </div>
    );
  }

  const r = load.value;
  const when = new Date(r.received_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const kind = (k: string) => t(`report.kind_${k}`);
  const published = r.status === 'published';
  return (
    <section class="mt-6 rounded-xl border border-hairline p-6" aria-labelledby="receipt-id-title">
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <h2 id="receipt-id-title" class="m-0 font-mono text-2xl font-medium">{r.id}</h2>
        <span class={`${PILL} ${TONE[r.status]}`}>{t(`report.status_${r.status}`)}</span>
        {r.backup_consent !== 'none' && (
          <span class={`${PILL} bg-[#f2ecfa] text-[#6b3fa0]`}>{t(`report.backup_${r.backup_consent}`)}</span>
        )}
      </div>

      <ol class="m-0 mt-5 grid list-none gap-3 p-0 text-[.9375rem]" aria-label={t('report.th_what')}>
        <Step done label={t('report.received', { channel: r.channel })} hint={`${when}${published ? ` · ${summary(r.facts)}` : ''}`} />
        <Step done={r.status !== 'pending'} now={r.status === 'pending'} label={t('report.step_review')} hint={t('report.step_review_hint')} />
        <Step done={published} label={t('report.step_published')} hint={t('report.step_published_hint')} />
      </ol>

      <div class="mt-5 overflow-x-auto">
        <table class="w-full border-collapse text-sm">
          <thead>
            <tr class="text-left text-xs tracking-[.06em] text-body-secondary uppercase">
              <th class="border-b border-hairline py-2 pe-3 font-semibold">{t('report.th_what')}</th>
              <th class="border-b border-hairline py-2 pe-3 font-semibold">{t('report.th_size')}</th>
              <th class="border-b border-hairline py-2 font-semibold">{t('report.th_who')}</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td class="border-b border-hairline py-2.5 pe-3">{kind('report')}</td>
              <td class="border-b border-hairline py-2.5 pe-3 tabular-nums">–</td>
              <td class="border-b border-hairline py-2.5">{t('report.who_public')}</td>
            </tr>
            {r.files.map((f) => (
              <tr key={`${f.kind}-${f.name}`}>
                <td class="border-b border-hairline py-2.5 pe-3">
                  {f.url ? <a href={f.url}>{kind(f.kind)}</a> : kind(f.kind)}
                  <span class="ms-1.5 font-mono text-xs text-body-secondary">{f.name}</span>
                </td>
                <td class="border-b border-hairline py-2.5 pe-3 whitespace-nowrap tabular-nums">{size(f.bytes)}</td>
                <td class="border-b border-hairline py-2.5">
                  {f.private
                    ? <span class={`${PILL} bg-[#f2ecfa] text-[#6b3fa0]`}>{t('report.who_private')}</span>
                    : t('report.who_public')}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {r.note && (
        <p class="m-0 mt-5 text-[.9375rem] whitespace-pre-wrap"><b class="font-semibold">{t('report.owner_note')}</b> {r.note}</p>
      )}
      {published && r.models.length > 0 && (
        <p class="m-0 mt-5 rounded-lg border border-hairline bg-surface-alt px-4 py-3 text-[.9375rem]">
          {t('report.filed_under')}{' '}
          {r.models.map((m, i) => (
            <span key={m.id}>{i > 0 && ', '}<a href={`${pathFor(locale, '/cameras/boards')}?model=${encodeURIComponent(m.id)}`}><b>{m.manufacturer} {m.model}</b></a></span>
          ))}
        </p>
      )}
      {!published && r.guess && (
        <p class="m-0 mt-5 flex flex-wrap items-baseline gap-x-3.5 gap-y-1 rounded-lg border border-hairline bg-surface-alt px-4 py-3 text-[.9375rem]">
          <span>{t('report.looks_like', { board: `${r.guess.manufacturer} ${r.guess.model}` }).split(`${r.guess.manufacturer} ${r.guess.model}`)
            .flatMap((part, i) => (i === 0 ? [part] : [<a key="b" href={`${pathFor(locale, '/cameras/boards')}?model=${encodeURIComponent(r.guess!.model_id)}`}><b>{r.guess!.manufacturer} {r.guess!.model}</b></a>, part]))}</span>
          <span class="text-[13.5px] text-body-secondary">{r.guess.why.join(' · ')} · {t('report.looks_like_why')}</span>
        </p>
      )}
    </section>
  );
}

function Step({ done, now, label, hint }: { done?: boolean; now?: boolean; label: string; hint: string }) {
  const dot = now ? 'border-accent bg-accent' : done ? 'border-ink bg-ink' : 'border-hairline bg-white';
  return (
    <li class="grid grid-cols-[auto_1fr] gap-x-3.5">
      <span class={`mt-1.5 size-3 rounded-full border-2 ${dot}`} aria-hidden="true" />
      <span>
        {label}
        <small class="block text-[13.5px] text-body-secondary">{hint}</small>
      </span>
    </li>
  );
}
