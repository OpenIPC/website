/**
 * What an XM device ID can be flashed with, as OpenIPC's own projects publish
 * it: coupler's image that moves the device to OpenIPC, and every stock build
 * the vendor published (mirrored by xmupdates), newest first, so a visitor
 * can go back to an earlier one when a build misbehaves. Where the vendor
 * has no build, a seller's own build (cctvsp.ru's, with the IPeye cloud) is
 * offered in its own block, named as the seller's and never as stock. Shared
 * by the board panel and the device-ID search.
 */
import type { BoardsT } from '../../lib/boards-i18n';
import { addsIPeye, formatBytes } from '../../lib/boards/model';
import type { VendorDevice, VendorFirmware } from '../../lib/boards/types';

const PYTHON_DVR = 'https://github.com/OpenIPC/python-dvr';
const RECOVERY = 'https://github.com/OpenIPC/wiki/blob/master/en/installation.md';
const BTN = 'shrink-0 self-start rounded-md border border-brand-blue px-3 py-1.5 text-sm font-medium whitespace-nowrap no-underline';

function day(iso: string | null, locale: string): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric' }).format(d);
}

function facts(f: VendorFirmware, locale: string, t: BoardsT, when: 'built' | 'archived' | 'updated'): string {
  const d = day(f.published_at, locale);
  return [f.size ? formatBytes(f.size, locale) : null, d ? t(`fw_${when}`, { date: d }) : null,
    f.sha256 ? `sha256 ${f.sha256.slice(0, 8)}…` : null].filter(Boolean).join(' · ');
}

export default function Firmware({ device, heading, note, locale, t }: {
  device: VendorDevice; heading: string; note?: string; locale: string; t: BoardsT;
}) {
  const { stock, coupler } = device;
  const sellers = device.sellers ?? [];
  return (
    <section class="overflow-hidden rounded-lg border border-hairline" aria-label={heading}>
      <header class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 bg-surface-alt px-3.5 py-2">
        <b class="font-mono">{heading}</b>
        {note && <span class="text-xs text-body-secondary">{note}</span>}
      </header>
      {!coupler && stock.length === 0 && sellers.length === 0 && <p class="m-0 px-3.5 py-3 text-sm text-body-secondary">{t('fw_none')}</p>}

      {coupler && (
        <div class="grid gap-2 border-b border-hairline px-3.5 py-3 last:border-b-0">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="grid min-w-0 gap-0.5">
              <span class="font-semibold">{t('fw_coupler')}</span>
              <span class="text-[13px] text-body-secondary">
                <span class="font-mono break-all text-body">{coupler.key}</span> · {facts(coupler, locale, t, 'built')} · OpenIPC/coupler
              </span>
            </div>
            <a class={`${BTN} bg-brand-blue text-white hover:text-white`} href={coupler.url}>{t('fw_download')}</a>
          </div>
          <div class="rounded-md bg-[#fff5e6] px-3 py-2 text-[13px] text-[#8a5200]">
            {t('fw_caution_flash')} <a href={PYTHON_DVR} class="text-inherit underline">python-dvr</a>. {t('fw_caution_before')}
            <ul class="mt-1 mb-0 list-disc pl-5">
              {stock.length > 0 && <li>{t('fw_caution_stock')}</li>}
              <li>{t('fw_caution_rollback')} (<a href={RECOVERY} class="text-inherit underline">{t('fw_recovery')}</a>);</li>
              <li>{t('fw_caution_firstboot_before')} <code class="font-mono">firstboot</code>{t('fw_caution_firstboot_after')}</li>
            </ul>
          </div>
        </div>
      )}

      {stock.length > 0 && (
        <div class="grid gap-2 px-3.5 py-3">
          <div class="grid gap-0.5">
            <span class="font-semibold">{t('fw_stock')}</span>
            <span class="text-[13px] text-body-secondary">
              {stock.length > 1 ? t('fw_stock_several') : t('fw_stock_one')}
            </span>
          </div>
          <ul class="m-0 grid list-none gap-1.5 p-0">
            {stock.map((f, i) => (
              <li key={`${f.key}-${f.version}`} class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-hairline px-2.5 py-2">
                <span class={`rounded-[5px] px-1.5 py-0.5 font-mono text-[11px] font-semibold tracking-wide uppercase ${i === 0 ? 'bg-[#e3f5ec] text-[#146c3c]' : 'bg-surface-alt text-body-secondary'}`}>
                  {i === 0 ? t('fw_latest') : t('fw_earlier')}
                </span>
                <span class="grid min-w-0 flex-1 gap-0.5">
                  <span class="font-mono text-[12.5px] break-all">{f.build} · {f.version}</span>
                  <span class="text-[12.5px] text-body-secondary">{facts(f, locale, t, 'archived')}</span>
                </span>
                <a class={`${BTN} text-brand-blue hover:border-link-hover`} href={f.url}>{t('fw_download')}</a>
              </li>
            ))}
          </ul>
          <span class="text-xs text-body-secondary">{t('fw_stock_source')}</span>
        </div>
      )}
      {sellers.length > 0 && (
        <div class="grid gap-2 border-t border-hairline px-3.5 py-3 first:border-t-0">
          <div class="grid gap-0.5">
            <span class="font-semibold">{t('fw_seller', { origin: sellers[0].origin ?? '' })}</span>
            <span class="text-[13px] text-body-secondary">
              {t('fw_seller_why', { origin: sellers[0].origin ?? '' })}
              {sellers.some(addsIPeye) && <> {t('fw_seller_ipeye')}</>}
            </span>
          </div>
          <ul class="m-0 grid list-none gap-1.5 p-0">
            {sellers.map((f) => (
              <li key={`${f.key}-${f.version}`} class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-hairline px-2.5 py-2">
                <span class="grid min-w-0 flex-1 gap-0.5">
                  <span class="font-mono text-[12.5px] break-all">{f.build}</span>
                  <span class="text-[12.5px] text-body-secondary">
                    {facts(f, locale, t, 'updated')}
                    {f.origin_url && <> · <a href={f.origin_url} class="text-inherit underline">{t('fw_seller_page', { origin: f.origin ?? '' })} ↗</a></>}
                  </span>
                </span>
                <a class={`${BTN} text-brand-blue hover:border-link-hover`} href={f.url}>{t('fw_download')}</a>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
