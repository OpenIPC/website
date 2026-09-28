/**
 * A build, picked by its day on a calendar. Only days with a build can be
 * chosen; a day with more than one build offers them by time, which is the
 * only place a reader meets a time or a commit.
 */
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { Build } from '../../lib/explorer/types';
import { byDay, dayOf, fmtDay, fmtDayShort, fmtMonth, monthGrid, monthOf, shiftMonth, timeOf, weekdays } from '../../lib/explorer/calendar';
import type { ExplorerT } from '../../lib/explorer-i18n';
import type { Locale } from '../../lib/i18n';

interface Props {
  id: string;
  labelId: string;
  /** Newest first. */
  builds: Build[];
  value: string | null;
  onChange: (id: string) => void;
  /** A build that cannot be picked: the one Compare with compares against. */
  exclude?: string | null;
  locale: Locale;
  t: ExplorerT;
}

const DAY = 'relative h-9 rounded-md text-[15px] tabular-nums';

export default function BuildPicker({ id, labelId, builds, value, onChange, exclude = null, locale, t }: Props) {
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState('');
  const [day, setDay] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const days = useMemo(() => byDay(builds), [builds]);
  const selected = builds.find((b) => b.id === value) ?? null;
  const excluded = exclude ? builds.find((b) => b.id === exclude) ?? null : null;
  const usable = (b: Build) => b.id !== exclude;
  const newest = builds.find(usable) ?? null;

  useEffect(() => {
    if (!open) return;
    const outside = (e: PointerEvent) => { if (root.current && !e.composedPath().includes(root.current)) setOpen(false); };
    const escape = (e: KeyboardEvent) => { if (e.key === 'Escape') { setOpen(false); button.current?.focus(); } };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); };
  }, [open]);

  if (builds.length === 0) return null;
  const first = monthOf(builds[builds.length - 1].built_at);
  const last = monthOf(builds[0].built_at);

  const toggle = () => {
    if (!open) {
      setMonth(monthOf((selected ?? builds[0]).built_at));
      setDay(null);
    }
    setOpen(!open);
  };
  const pick = (b: Build) => {
    setOpen(false);
    button.current?.focus();
    onChange(b.id);
  };

  const selectedDay = selected ? dayOf(selected.built_at) : null;
  const shownDay = day ?? (selectedDay && (days.get(selectedDay)?.length ?? 0) > 1 ? selectedDay : null);
  const shown = shownDay ? days.get(shownDay) ?? [] : [];
  const grid = open ? monthGrid(month) : null;

  return (
    <div class="relative min-w-0" ref={root}>
      <button type="button" id={id} ref={button} aria-haspopup="dialog" aria-expanded={open} aria-labelledby={`${labelId} ${id}`}
        class="inline-flex max-w-full cursor-pointer items-center gap-2 rounded-md border border-hairline bg-white px-2.5 py-1.5 text-left text-[15px]"
        onClick={toggle}>
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" aria-hidden="true" class="shrink-0 text-body-secondary">
          <rect x="2" y="3" width="12" height="11" rx="2" /><path d="M2 6.5h12M5.5 1.5v3M10.5 1.5v3" />
        </svg>
        {selected ? (
          <span class="whitespace-nowrap">
            {fmtDay(dayOf(selected.built_at), locale)}
            {(days.get(dayOf(selected.built_at))?.length ?? 0) > 1 && <span class="text-body-secondary"> · {timeOf(selected.built_at)}</span>}
          </span>
        ) : <span>—</span>}
        {selected && selected.id === builds[0].id && !exclude && (
          <span class="rounded-full bg-surface-alt px-2 text-xs font-medium text-brand-blue">{t('newest')}</span>
        )}
      </button>

      {open && grid && (
        <div role="dialog" aria-label={t('calendar_label')}
          class="absolute top-[calc(100%+6px)] left-0 z-20 w-[20rem] max-w-[calc(100vw-2rem)] rounded-lg border border-hairline bg-white p-3 shadow-[0_8px_28px_rgba(20,28,60,.14)]">
          <div class="mb-1.5 flex items-center justify-between">
            <MonthButton label={t('calendar_prev')} disabled={month <= first} onClick={() => setMonth(shiftMonth(month, -1))}>‹</MonthButton>
            <strong class="font-semibold">{fmtMonth(month, locale)}</strong>
            <MonthButton label={t('calendar_next')} disabled={month >= last} onClick={() => setMonth(shiftMonth(month, 1))}>›</MonthButton>
          </div>
          <div class="grid grid-cols-7 gap-0.5 text-center">
            {weekdays(locale).map((w) => <div key={w} class="py-1 text-[11px] font-semibold text-[#8a93a3] uppercase">{w}</div>)}
            {Array.from({ length: grid.lead }, (_, i) => <div key={`lead-${i}`} />)}
            {grid.days.map((d) => {
              const list = days.get(d) ?? [];
              const pickable = list.filter(usable);
              const on = d === selectedDay || d === day;
              const n = Number(d.slice(8));
              const label = `${fmtDayShort(d, locale)}: ${list.length ? t('calendar_day_builds', { count: list.length }) : t('calendar_no_build')}`;
              if (pickable.length === 0) {
                return (
                  <div key={d} aria-label={label}
                    class={`${DAY} flex items-center justify-center ${excluded && d === dayOf(excluded.built_at) ? 'text-body shadow-[inset_0_0_0_1.5px_#8a93a3]' : 'text-[#c4c9d3]'}`}>{n}</div>
                );
              }
              return (
                <button key={d} type="button" aria-label={label} aria-pressed={on}
                  class={`${DAY} cursor-pointer font-medium ${on ? 'bg-brand-blue text-white' : 'text-body hover:bg-surface-alt'} ${excluded && d === dayOf(excluded.built_at) ? 'shadow-[inset_0_0_0_1.5px_#8a93a3]' : ''}`}
                  onClick={() => (pickable.length === 1 ? pick(pickable[0]) : setDay(d))}>
                  {n}
                  {list.length > 1 && <span aria-hidden="true" class={`absolute bottom-1 left-1/2 -ml-0.5 size-1 rounded-full ${on ? 'bg-white' : 'bg-brand-blue'}`} />}
                </button>
              );
            })}
          </div>
          {shownDay && shown.length > 1 && (
            <div class="mt-2 grid gap-1.5 border-t border-hairline pt-2">
              <p class="m-0 text-[13px] text-body-secondary">{t('calendar_builds_on', { count: shown.length, day: fmtDayShort(shownDay, locale) })}</p>
              <div class="flex flex-wrap gap-1.5">
                {[...shown].reverse().map((b) => (
                  <button key={b.id} type="button" disabled={!usable(b)} title={b.id}
                    class={`cursor-pointer rounded-md border px-2 py-1 text-sm whitespace-nowrap tabular-nums disabled:cursor-default disabled:opacity-45 ${b.id === value ? 'border-brand-blue bg-surface-alt' : 'border-hairline bg-white'}`}
                    onClick={() => pick(b)}>
                    {timeOf(b.built_at)}<small class="ml-1.5 font-mono text-xs text-body-secondary">{b.short}</small>
                  </button>
                ))}
              </div>
            </div>
          )}
          <div class="mt-2 flex items-center justify-between gap-2 text-[13px] text-body-secondary">
            <span>{t('calendar_span', { count: builds.length, first: fmtDayShort(dayOf(builds[builds.length - 1].built_at), locale), last: fmtDayShort(dayOf(builds[0].built_at), locale) })}</span>
            {newest && (
              <button type="button" class="cursor-pointer px-1 text-brand-blue" onClick={() => pick(newest)}>
                {exclude ? t('calendar_newest_other') : t('calendar_newest')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function MonthButton({ label, disabled, onClick, children }: { label: string; disabled: boolean; onClick: () => void; children: string }) {
  return (
    <button type="button" aria-label={label} disabled={disabled} onClick={onClick}
      class="size-8 cursor-pointer rounded-md text-xl text-body hover:bg-surface-alt disabled:cursor-default disabled:text-[#c4c9d3] disabled:hover:bg-transparent">
      {children}
    </button>
  );
}
