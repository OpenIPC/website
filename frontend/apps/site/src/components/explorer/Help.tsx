/** The explorer's help, as an overlay that closes on Escape or the backdrop. */
import { useEffect, useRef } from 'preact/hooks';
import type { ExplorerT } from '../../lib/explorer-i18n';

/** The order the sections read in; the copy is data/locales/explorer.en.yml. */
export const HELP_SECTIONS = ['what', 'first_look', 'flash_map', 'tabs', 'what_if', 'links'] as const;

export default function Help({ onClose, t }: { onClose: () => void; t: ExplorerT }) {
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    close.current?.focus();
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto p-4 sm:p-8" role="dialog" aria-modal="true" aria-labelledby="explorer-help-title">
      <div class="fixed inset-0 bg-ink/60" onClick={onClose} />
      <div class="relative w-full max-w-3xl rounded-lg bg-white shadow-xl">
        <header class="flex items-center justify-between gap-4 border-b border-hairline px-5 py-4">
          <h2 id="explorer-help-title" class="m-0 text-xl font-semibold">{t('help_title')}</h2>
          <button ref={close} type="button" class="cursor-pointer rounded px-2 text-2xl leading-none" aria-label={t('help_close')} onClick={onClose}>×</button>
        </header>
        <div class="px-5 py-4">
          {HELP_SECTIONS.map((id) => (
            <section key={id} class="mb-5 last:mb-0">
              <h3 class="mt-0 mb-2 text-base font-semibold">{t(`help.${id}.title`)}</h3>
              <div class="explorer-help max-w-[70ch] text-[15px] text-body [&_code]:font-mono [&_code]:text-[13px] [&_li]:mb-1 [&_ol]:pl-5 [&_p]:mb-2"
                dangerouslySetInnerHTML={{ __html: t(`help.${id}.html`) }} />
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
