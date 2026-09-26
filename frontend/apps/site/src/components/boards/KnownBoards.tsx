/**
 * "Known boards with <SoC>", under the installation wizard on each SoC page.
 *
 * It reads the same /api/v1/boards the catalogue page does and shows the
 * boards built on this chip. A SoC with no boards on record, a catalogue that
 * cannot be fetched, or a page still loading all render nothing: the wizard
 * is what the visitor came for, and this section must never make that page
 * look broken.
 */
import { useEffect, useState } from 'preact/hooks';
import { fetchBoards } from '../../lib/boards/api';
import { entries, frontPhoto, has, type Entry } from '../../lib/boards/model';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { Chip, Thumb } from './parts';

const SHOWN = ['pinout', 'flash_dump', 'uboot_env'] as const;

export default function KnownBoards({ locale, soc, model, catalogueHref }: {
  locale: Locale; soc: string; model: string; catalogueHref: string;
}) {
  const t = useBoardsTranslations(locale);
  const [boards, setBoards] = useState<Entry[]>([]);

  useEffect(() => {
    let live = true;
    fetchBoards()
      .then((file) => live && setBoards(entries(file).filter((m) => m.soc === soc)))
      .catch(() => { /* Nothing to add to the page; the wizard stands alone. */ });
    return () => { live = false; };
  }, [soc]);

  if (boards.length === 0) return null;

  return (
    <section class="site-container mt-12 mb-12 grid gap-3.5" aria-labelledby="known-boards">
      <header class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 id="known-boards" class="mb-0 text-h3 font-semibold">
          {t('known_title', { soc: model })} <small class="text-sm font-normal text-body-secondary">({boards.length})</small>
        </h2>
        <a href={`${catalogueHref}?soc=${encodeURIComponent(soc)}`}>{t('known_link')}</a>
      </header>
      <div class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,260px),1fr))] gap-3.5">
        {boards.map((m) => {
          const title = m.model ?? t('unidentified');
          const photo = frontPhoto(m);
          const sensors = [...new Set(m.units.map((u) => u.sensor).filter(Boolean))].join('; ') || t('unknown');
          const maker = m.maker.id === 'unknown' ? t('unknown_maker') : m.maker.name;
          return (
            <div key={m.id} class="grid grid-cols-[110px_1fr] overflow-hidden rounded-lg border border-hairline bg-white">
              {photo
                ? <Thumb file={photo} class="h-full min-h-[96px] w-[110px]" alt={t('photo_alt', { what: t(`tag_${photo.kind}`), board: title })} />
                : <span class="bg-surface-alt" />}
              <div class="grid min-w-0 content-start gap-1 px-3 py-2.5 text-[13px]">
                <b class={m.model ? 'font-mono text-sm font-semibold break-all' : 'text-sm font-medium text-body-secondary'}>{title}</b>
                <span class="text-body-secondary">{maker} · {sensors}</span>
                <div class="flex flex-wrap gap-1.5">
                  {SHOWN.map((k) => <Chip key={k} ok={has(m, k)}>{t(`cov_${k}`)}</Chip>)}
                </div>
              </div>
            </div>
          );
        })}
      </div>
      <p class="m-0 text-[13px] text-body-secondary" dangerouslySetInnerHTML={{ __html: t('known_credit_html') }} />
    </section>
  );
}
