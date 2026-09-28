/**
 * "Known boards with <SoC>", under the installation wizard on each SoC page.
 *
 * It reads /api/v1/boards?soc=<urlname>, the boards built on this chip, in the
 * page's language. Each card opens that board's details in the catalogue. A
 * SoC with no boards on record, a catalogue that cannot be fetched, or a page
 * still loading all render nothing: the wizard is what the visitor came for,
 * and this section must never make that page look broken.
 */
import { Fragment } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { fetchBoards } from '../../lib/boards/api';
import { HEADING_CLASS, entries, frontPhoto, has, heading, newestFirst, subtitle, tally, type Entry } from '../../lib/boards/model';
import type { Source } from '../../lib/boards/types';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { Chip, makerName } from './parts';

const SHOWN = ['pinout', 'flash_dump', 'uboot_env'] as const;

export default function KnownBoards({ locale, soc, model, catalogueHref }: {
  locale: Locale; soc: string; model: string; catalogueHref: string;
}) {
  const t = useBoardsTranslations(locale);
  const [boards, setBoards] = useState<Entry[]>([]);
  const [sources, setSources] = useState<Source[]>([]);

  useEffect(() => {
    let live = true;
    fetchBoards(locale, soc)
      .then((file) => {
        if (!live) return;
        // The server answers for this SoC; the check keeps a server that ignored ?soc= from filling the page.
        const mine = newestFirst(entries(file).filter((m) => m.soc === soc));
        const used = new Set(mine.flatMap((m) => m.sources));
        setBoards(mine);
        setSources(file.sources.filter((s) => used.has(s.id)));
      })
      .catch(() => { /* Nothing to add to the page; the wizard stands alone. */ });
    return () => { live = false; };
  }, [soc, locale]);

  if (boards.length === 0) return null;

  return (
    <section class="site-container mt-12 mb-12 grid gap-3.5" aria-labelledby="known-boards">
      <header class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 id="known-boards" class="mb-0 text-h3 font-semibold">
          {t('known_title', { soc: model })} <small class="text-sm font-normal text-body-secondary">({tally(boards, t)})</small>
        </h2>
        <a href={`${catalogueHref}?soc=${encodeURIComponent(soc)}`}>{t('known_link')}</a>
      </header>
      <ul class="m-0 grid list-none grid-cols-[repeat(auto-fill,minmax(min(100%,260px),1fr))] gap-3.5 p-0">
        {boards.map((m) => {
          const head = heading(m);
          const title = head.text ?? t('unidentified');
          const name = subtitle(m);
          const photo = frontPhoto(m);
          const sensors = [...new Set(m.units.map((u) => u.sensor).filter(Boolean))].join('; ') || t('unknown');
          return (
            <li key={m.id}>
              <a href={`${catalogueHref}?model=${encodeURIComponent(m.id)}`} aria-label={t('details_of', { board: title })}
                class="grid h-full grid-cols-[110px_1fr] overflow-hidden rounded-lg border border-hairline bg-white text-body no-underline transition-colors hover:border-brand-blue">
                {photo
                  ? <img src={photo.thumb_url} alt="" loading="lazy" decoding="async" class="block h-full min-h-[96px] w-[110px] bg-surface-alt object-cover" />
                  : <span class="bg-surface-alt" />}
                <span class="grid min-w-0 content-start gap-1 px-3 py-2.5 text-[13px]">
                  <b class={`text-sm ${HEADING_CLASS[head.kind]}`}>{title}</b>
                  {name && <span class="leading-snug">{name}</span>}
                  <span class="text-body-secondary">
                    {m.kind && m.kind !== 'board' && <b class="font-semibold text-[#8a4b00]">{t(`kind_${m.kind}`)} · </b>}
                    {makerName(m.maker.id, m.maker.name, t)} · {sensors}
                  </span>
                  <span class="flex flex-wrap gap-1.5">
                    {SHOWN.map((k) => <Chip key={k} ok={has(m, k)}>{t(`cov_${k}`)}</Chip>)}
                  </span>
                </span>
              </a>
            </li>
          );
        })}
      </ul>
      {sources.length > 0 && (
        <p class="m-0 text-[13px] text-body-secondary">
          {t('known_sources')}
          {sources.map((s, i) => (
            <Fragment key={s.id}>
              {i > 0 && ', '}
              <a href={s.url} target="_blank" rel="noopener">{s.name}</a>
            </Fragment>
          ))}.
        </p>
      )}
    </section>
  );
}
