/**
 * The firmware a maker whose builds are keyed by board model (Anjoy Vision)
 * made for one board, mirrored by OpenIPC/anjoyupdates: grouped by the
 * device type each build is for, newest first, the maker's own builds told
 * apart from those made for a customer's app, with the maker's own notes on
 * flashing them.
 */
import { useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { buildGroups, formatBytes, formatDay } from '../../lib/boards/model';
import type { ModelBuild } from '../../lib/boards/types';

const BTN = 'shrink-0 self-start rounded-md border border-brand-blue px-3 py-1 text-sm font-medium whitespace-nowrap no-underline text-brand-blue hover:border-link-hover';
const SHOWN = 4;

export default function ModelFirmware({ builds, maker, locale, t }: {
  builds: ModelBuild[]; maker: string; locale: string; t: BoardsT;
}) {
  const [all, setAll] = useState(false);
  const groups = buildGroups(builds);
  const shown = all ? groups : groups.slice(0, SHOWN);
  const types = (list: typeof groups) => new Set(list.map((g) => g.deviceType)).size;
  const hidden = types(groups) - types(shown);
  return (
    <section aria-labelledby="board-panel-model-firmware" class="grid gap-3">
      <h3 id="board-panel-model-firmware" class="mb-0 text-base font-semibold">
        {t('mfw_title', { maker })}{' '}
        <span class="text-sm font-normal text-body-secondary">· {t('mfw_builds', { count: builds.length })} · {t('mfw_types', { count: types(groups) })}</span>
      </h3>
      <p class="m-0 text-[13px] text-body-secondary">{t('mfw_lead')}</p>
      <div class="rounded-md bg-[#fff5e6] px-3 py-2 text-[13px] text-[#8a5200]"><b>{t('mfw_notes_label', { maker })}</b> {t('mfw_notes')}</div>
      {shown.map((g) => (
        <div key={`${g.deviceType} ${g.variant ?? ''}`} class="overflow-hidden rounded-md border border-hairline">
          <div class="flex flex-wrap items-baseline gap-x-2 bg-surface-alt px-2.5 py-1.5">
            <b class="font-mono text-[13px] break-all">{g.deviceType}</b>
            {g.builds[0].category && <span class="text-xs text-body-secondary">{t(`mfw_cat_${g.builds[0].category}`)}</span>}
            {g.variant && <span class="text-xs text-body-secondary">· {g.variant}</span>}
          </div>
          {g.builds.map((b, i) => (
            <div key={b.key} class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-hairline px-2.5 py-1.5 text-[13px]">
              {i === 0 && <span class="rounded-[5px] bg-[#e3f5ec] px-1.5 py-0.5 font-mono text-[11px] font-semibold tracking-wide text-[#146c3c] uppercase">{t('fw_latest')}</span>}
              <span class="font-mono">{b.version}</span>
              {b.app === 'public'
                ? <span class="rounded bg-[#e3f5ec] px-1.5 text-xs font-semibold text-[#146c3c]">{t('mfw_own', { maker })}</span>
                : <span class="rounded bg-[#fdf1e3] px-1.5 text-xs font-semibold text-[#8a4b00]">{t('mfw_customer', { app: b.app })}</span>}
              <span class="text-body-secondary">
                {[formatDay(b.published_at, locale, true), b.size ? formatBytes(b.size, locale) : null, b.sha256 ? `sha256 ${b.sha256.slice(0, 8)}…` : null].filter(Boolean).join(' · ')}
                {b.collection && <> · {t(`mfw_collection_${b.collection}`, { maker })}</>}
              </span>
              <a class={`${BTN} ml-auto`} href={b.url}>{t('fw_download')}</a>
              <span class="basis-full font-mono text-xs break-all text-body-secondary">{b.build}</span>
            </div>
          ))}
        </div>
      ))}
      {!all && hidden > 0 && (
        <button type="button" class="justify-self-start text-sm text-brand-blue" onClick={() => setAll(true)}>
          {t('mfw_more', { count: hidden })}
        </button>
      )}
      <span class="text-xs text-body-secondary">{t('mfw_source', { maker })}</span>
    </section>
  );
}
