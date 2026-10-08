/**
 * /crashes: the kernel crashes cameras recovered from, one row per bug,
 * worst first (service/internal/crashes). Public: the signature, where it was
 * seen and what is being done about it -- never a log, a camera or a member.
 * A row opens to the chips and builds it was seen on.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { fetchSignature, fetchSignatures, frame, KIND_TONE, STATUS_TONE, type Combo, type Signature } from '../../lib/crashes';

export default function CrashList({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [list, setList] = useState<Signature[] | null>(null);
  const [error, setError] = useState(false);
  const [open, setOpen] = useState<string | null>(null);

  useEffect(() => {
    fetchSignatures().then((r) => {
      setList(r.signatures);
      const hash = location.hash.slice(1);
      if (/^[0-9a-f]{12}$/.test(hash)) setOpen(hash);
    }).catch(() => setError(true));
  }, []);
  useEffect(() => {
    if (open && list) document.getElementById(open)?.scrollIntoView({ block: 'nearest' });
  }, [open, list !== null]);

  return (
    <section class="mt-8 grid gap-3" aria-label={t('crashes.list_label')}>
      {error && <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {!error && list === null && <div class="h-48 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />}
      {list && list.length === 0 && <p class="m-0 text-body-secondary">{t('crashes.empty')}</p>}
      {list && list.map((g) => (
        <Row key={g.id} g={g} t={t} open={open === g.id} onToggle={() => {
          const next = open === g.id ? null : g.id;
          setOpen(next);
          history.replaceState(null, '', next ? `#${next}` : location.pathname);
        }} />
      ))}
      <HowToSend locale={locale} t={t} />
    </section>
  );
}

function Row({ g, t, open, onToggle }: { g: Signature; t: BoardsT; open: boolean; onToggle: () => void }) {
  const hardware = [...g.socs, ...g.sensors].join(' · ').toUpperCase();
  return (
    <article id={g.id} class={`grid gap-2 rounded-lg border p-3 ${open ? 'border-brand-blue' : 'border-hairline'}`}>
      <button type="button" class="grid cursor-pointer gap-1.5 bg-transparent p-0 text-left" aria-expanded={open} onClick={onToggle}>
        <span class="flex flex-wrap items-center gap-2">
          <span class={`rounded-full px-2.5 py-0.5 text-[12px] font-semibold ${KIND_TONE[g.kind]}`}>
            {t(`crashes.kind_${g.kind}`)}{g.in_irq && g.kind !== 'warning' ? ` · ${t('crashes.in_irq')}` : ''}
          </span>
          <span class={`rounded-full px-2.5 py-0.5 text-[12px] font-semibold ${STATUS_TONE[g.status]}`}>
            {t(`crashes.status_${g.status}`)}{g.status === 'fixed' && g.fixed_in ? ` · ${g.fixed_in}` : ''}
          </span>
          {g.current && <span class="rounded-full bg-[#ecebff] px-2.5 py-0.5 text-[12px] font-semibold text-[#4434b8]">{t('crashes.current')}</span>}
        </span>
        <span class="font-mono text-[14px] font-semibold break-words text-ink">{g.title}</span>
        <span class="flex flex-wrap gap-x-4 gap-y-0.5 text-[13px] text-body-secondary">
          <span>{t('crashes.cameras', { count: g.cameras })}</span>
          {hardware && <span class="font-mono text-[12px]">{hardware}</span>}
          <span>{t('crashes.last_seen', { date: g.last_seen.slice(0, 10) })}</span>
        </span>
      </button>
      {open && <Open g={g} t={t} />}
    </article>
  );
}

function Open({ g, t }: { g: Signature; t: BoardsT }) {
  const [combos, setCombos] = useState<Combo[] | null>(null);
  useEffect(() => { fetchSignature(g.id).then((r) => setCombos(r.seen_on)).catch(() => setCombos([])); }, [g.id]);
  return (
    <div class="grid gap-2 border-t border-hairline pt-2 text-[13px]">
      <pre class="m-0 overflow-x-auto rounded-md bg-surface-alt p-2.5 font-mono text-[12px] leading-relaxed">
        {g.frames.slice(0, 8).map((f, i) => `${i === 0 ? '' : '← '}${frame(f)}`).join('\n')}
      </pre>
      {g.issue_url && <a href={g.issue_url} rel="noopener">{t('crashes.issue')}</a>}
      {combos && combos.length > 0 && (
        <table class="w-full border-collapse">
          <thead>
            <tr class="text-left text-[12px] text-body-secondary">
              <th class="py-1 font-medium">{t('crashes.col_soc')}</th>
              <th class="py-1 font-medium">{t('crashes.col_sensor')}</th>
              <th class="py-1 text-right font-medium">{t('crashes.col_cameras')}</th>
            </tr>
          </thead>
          <tbody>
            {combos.map((c) => (
              <tr key={`${c.soc}/${c.sensor}`} class="border-t border-hairline">
                <td class="py-1 font-mono">{c.soc || '?'}</td>
                <td class="py-1 font-mono">{c.sensor || '?'}</td>
                <td class="py-1 text-right tabular-nums">{c.cameras}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p class="m-0 text-[12px] text-body-secondary">
        {[t('crashes.signature', { id: g.id }), g.firmware.length > 0 && t('crashes.firmware', { list: g.firmware.slice(0, 4).join(', ') })]
          .filter(Boolean).join(' · ')}
      </p>
    </div>
  );
}

function HowToSend({ locale, t }: { locale: Locale; t: BoardsT }) {
  return (
    <aside class="mt-4 grid max-w-[720px] gap-2 rounded-xl border border-hairline bg-surface-alt p-5 text-[.9375rem]">
      <h2 class="m-0 text-lg font-semibold">{t('crashes.how_title')}</h2>
      <ol class="m-0 grid gap-1.5 ps-5">
        <li>{t('crashes.how_1')}</li>
        <li>{t('crashes.how_2')} <a href={`${pathFor(locale, '/club')}#crashes`}>{t('crashes.how_club')}</a></li>
        <li>{t('crashes.how_3')}</li>
      </ol>
      <p class="m-0 text-[12.5px] text-body-secondary">{t('crashes.how_privacy')}</p>
    </aside>
  );
}
