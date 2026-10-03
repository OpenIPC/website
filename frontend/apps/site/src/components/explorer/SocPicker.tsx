/**
 * The SoC, picked by typing any part of its name: `335` finds ssc335 and
 * ssc335de, `sigma` every SigmaStar. A native select's type-ahead matched
 * only from the start, forgot what was typed after a second, and sent a lone
 * digit to an unrelated chip (searchSocs says more). An ARIA combobox: the
 * arrows move, Enter picks, Escape puts the box back as it was.
 */
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { VendorGroup } from '../../lib/explorer/platforms';
import { searchSocs } from '../../lib/explorer/socsearch';
import type { ExplorerT } from '../../lib/explorer-i18n';

interface Props {
  id: string;
  labelId: string;
  groups: VendorGroup[];
  value: string | null;
  onChange: (soc: string) => void;
  t: ExplorerT;
}

export default function SocPicker({ id, labelId, groups, value, onChange, t }: Props) {
  const [open, setOpen] = useState(false);
  /** What the reader typed since opening; null until they type, which offers every chip. */
  const [query, setQuery] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);

  const shown = useMemo(() => searchSocs(groups, query ?? ''), [groups, query]);
  const flat = useMemo(() => shown.flatMap((g) => g.socs), [shown]);
  const listId = `${id}-list`;
  const optionId = (s: string) => `${id}-opt-${s}`;

  useEffect(() => {
    if (!open || !list.current) return;
    list.current.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [open, active, flat]);

  const show = () => {
    if (open) return;
    setQuery(null);
    setActive(Math.max(0, groups.flatMap((g) => g.socs).indexOf(value ?? '')));
    setOpen(true);
  };
  const close = () => {
    setOpen(false);
    setQuery(null);
  };
  const pick = (s: string) => {
    close();
    if (s !== value) onChange(s);
  };

  const onKeyDown = (e: KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp': {
        e.preventDefault();
        if (!open) { show(); return; }
        const step = e.key === 'ArrowDown' ? 1 : -1;
        if (flat.length) setActive((a) => (a + step + flat.length) % flat.length);
        return;
      }
      case 'Home':
      case 'End':
        if (open && query === null && flat.length) { e.preventDefault(); setActive(e.key === 'Home' ? 0 : flat.length - 1); }
        return;
      case 'Enter':
        if (open) {
          e.preventDefault();
          if (flat[active]) pick(flat[active]);
        }
        return;
      case 'Escape':
        if (open) { e.preventDefault(); close(); input.current?.select(); }
        return;
    }
  };

  return (
    <div class="relative min-w-0">
      <input id={id} ref={input} type="text" role="combobox" autocomplete="off" spellcheck={false}
        aria-labelledby={labelId} aria-expanded={open} aria-controls={listId} aria-autocomplete="list"
        aria-activedescendant={open && flat[active] ? optionId(flat[active]) : undefined}
        placeholder={t('soc_search')}
        class="w-[11.5rem] max-w-full rounded-md border border-hairline bg-white py-1.5 pr-8 pl-2.5 text-[15px]"
        value={open && query !== null ? query : value ?? ''}
        onFocus={(e) => { show(); (e.target as HTMLInputElement).select(); }}
        onClick={show}
        onBlur={close}
        onInput={(e) => { setQuery((e.target as HTMLInputElement).value); setActive(0); setOpen(true); }}
        onKeyDown={onKeyDown} />
      <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
        class="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-body-secondary">
        <path d="M2.5 4.5 6 8l3.5-3.5" />
      </svg>
      {/* Mounted before anything changes, so a screen reader hears what typing left: how many chips, or none. */}
      <p role="status" class="sr-only">{open && query !== null ? (flat.length ? t('soc_count', { count: flat.length }) : t('soc_none')) : ''}</p>
      {open && flat.length === 0 && (
        <p aria-hidden="true"
          class="absolute top-[calc(100%+6px)] left-0 z-20 m-0 w-[14rem] max-w-[calc(100vw-2rem)] rounded-lg border border-hairline bg-white px-3.5 py-2.5 text-sm text-body-secondary shadow-[0_8px_28px_rgba(20,28,60,.14)]">
          {t('soc_none')}
        </p>
      )}
      {open && flat.length > 0 && (
        <ul id={listId} ref={list} role="listbox" aria-labelledby={labelId}
          class="absolute top-[calc(100%+6px)] left-0 z-20 m-0 max-h-80 w-[14rem] max-w-[calc(100vw-2rem)] list-none overflow-auto rounded-lg border border-hairline bg-white p-1 shadow-[0_8px_28px_rgba(20,28,60,.14)]">
          {shown.map((g) => (
            <li key={g.vendor ?? ''} role="presentation">
              <div role="presentation" class="px-2.5 pt-1.5 pb-0.5 text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">
                {g.vendor ?? t('soc_other')}
              </div>
              <ul role="group" aria-label={g.vendor ?? t('soc_other')} class="m-0 list-none p-0">
                {g.socs.map((s) => {
                  const i = flat.indexOf(s);
                  return (
                    <li key={s} id={optionId(s)} role="option" aria-selected={i === active}
                      class={`cursor-pointer rounded-md px-2.5 py-1 text-[15px] ${i === active ? 'bg-surface-alt' : ''} ${s === value ? 'font-semibold text-brand-blue' : ''}`}
                      // Picked on click, which assistive technology sends without a
                      // mousedown; the mousedown only keeps focus in the box, whose
                      // blur would otherwise close the list before the click lands.
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => pick(s)}
                      onMouseMove={() => i !== active && setActive(i)}>
                      {s}
                    </li>
                  );
                })}
              </ul>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
