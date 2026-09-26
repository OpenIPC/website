/**
 * What a build does with the chip: the flash map, drawn to the report's flash
 * size and caps, and the kernel and root-filesystem meters beside it.
 */
import type { Sizes } from '../../lib/explorer/types';
import { flashSegments, headroomState, hexOffset, needsMoreThanEight, type HeadroomState, type SegmentKind } from '../../lib/explorer/summary';
import { fmtBytes, fmtKiB, fmtNum, fmtPct } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';

const SEGMENT_CLASS: Record<SegmentKind, string> = {
  boot: 'bg-[#6b7385] text-white',
  kernel: 'bg-brand-blue text-white',
  rootfs: 'bg-[#2f9e8f] text-white',
  overlay: 'bg-[#c9ceda] text-body',
};

const STATE_PILL: Record<HeadroomState, string> = {
  ok: 'bg-[#e3f3ea] text-green',
  warn: 'bg-[#fbf0dd] text-[#8a5300]',
  crit: 'bg-[#fbe4e6] text-red',
  over: 'bg-red text-white',
  unknown: 'bg-surface-alt text-body-secondary',
};

const STATE_BAR: Record<HeadroomState, string> = {
  ok: 'bg-brand-blue',
  warn: 'bg-[#b26b00]',
  crit: 'bg-red',
  over: 'bg-red',
  unknown: 'bg-hairline',
};

const kib = (n: number) => fmtNum(n);

export default function Summary({ sizes, t }: { sizes: Sizes; t: ExplorerT }) {
  const segs = flashSegments(sizes);
  const total = (sizes.flash_mb ?? 0) * 1024;

  return (
    <section class="grid items-start gap-7 lg:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)]">
      <div class="min-w-0">
        <div class="mb-2.5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <strong class="text-[22px] font-semibold">{sizes.board}{sizes.variant ? ` · ${sizes.variant}` : ''}</strong>
          <span class="text-sm text-body-secondary">
            {sizes.flash_mb ? t('flash_layout', { mb: sizes.flash_mb }) : ''}
            {sizes.flash_mb && sizes.kernel_version ? ' · ' : ''}
            {sizes.kernel_version ? t('kernel_version', { version: sizes.kernel_version }) : ''}
          </span>
        </div>
        {segs && (
          <>
            <div class="flex h-[46px] overflow-hidden rounded-md border border-hairline" role="img"
              aria-label={segs.map((s) => `${t(`segment_${s.kind}`)} ${fmtKiB(s.sizeKb)}`).join(', ')}>
              {segs.map((s) => (
                <div
                  key={s.kind}
                  class={`relative flex items-center justify-center overflow-hidden whitespace-nowrap text-xs ${SEGMENT_CLASS[s.kind]}`}
                  style={{ flexBasis: `${(s.sizeKb / total) * 100}%` }}
                  title={`${t(`legend_${s.kind}`)}: ${fmtKiB(s.sizeKb)}${s.usedKb != null ? `, ${t('segment_used', { kb: fmtKiB(s.usedKb) })}` : ''}`}
                >
                  {s.usedKb != null && (
                    <i class="absolute inset-y-0 left-0 bg-black/20" style={{ width: `${Math.min(s.usedKb / s.sizeKb, 1) * 100}%` }} />
                  )}
                  {s.sizeKb / total > 0.07 && <span class="relative px-1.5">{t(`segment_${s.kind}`)}</span>}
                </div>
              ))}
            </div>
            <div class="mt-1.5 flex flex-wrap justify-between gap-2 font-mono text-xs text-[#8a93a3] tabular-nums">
              {segs.map((s) => <span key={s.kind}>{hexOffset(s.offsetKb)}</span>)}
              <span>{hexOffset(total)}</span>
            </div>
            <div class="mt-1 flex flex-wrap gap-x-4 gap-y-1.5 text-[13px] text-body-secondary">
              {(['boot', 'kernel', 'rootfs', 'overlay'] as const).map((k) => (
                <span key={k}><i class={`mr-1.5 inline-block size-2.5 rounded-sm align-[-1px] ${SEGMENT_CLASS[k].split(' ')[0]}`} />{t(`legend_${k}`)}</span>
              ))}
              <span>{t('legend_used')}</span>
            </div>
          </>
        )}
        <div class="mt-3.5 flex flex-wrap gap-x-[18px] gap-y-1.5 text-sm text-body-secondary">
          <span>{t('fact_installed')} <b class="font-medium text-body">{fmtBytes(sizes.rootfs.uncompressed_bytes)}</b></span>
          {sizes.rootfs.compressed_bytes != null && (
            <span>
              {t('fact_compressed')} <b class="font-medium text-body">{fmtBytes(sizes.rootfs.compressed_bytes)}</b>
              {sizes.rootfs.compression ? ` (${sizes.rootfs.compression}${sizes.rootfs.compression_ratio ? `, ${fmtPct(sizes.rootfs.compression_ratio * 100, 0)}` : ''})` : ''}
            </span>
          )}
          <span>{t('fact_counts', { packages: sizes.packages.length, modules: sizes.linux_components.modules.length })}</span>
        </div>
        {needsMoreThanEight(sizes) && (
          <p class="mt-3.5 mb-0 border-l-[3px] border-[#b26b00] bg-[#fbf0dd] px-3 py-2 text-sm">
            {t('needs_16mb', { mb: sizes.flash_mb, rootfs: kib(sizes.headroom.rootfs.used_kb ?? 0) })}
          </p>
        )}
      </div>
      <div class="grid gap-3.5">
        <Meter title={t('meter_kernel')} used={sizes.headroom.kernel.used_kb} cap={sizes.headroom.kernel.cap_kb} t={t} />
        <Meter title={t('meter_rootfs')} used={sizes.headroom.rootfs.used_kb} cap={sizes.headroom.rootfs.cap_kb} t={t} />
      </div>
    </section>
  );
}

function Meter({ title, used, cap, t }: { title: string; used: number | null; cap: number | null; t: ExplorerT }) {
  const state = headroomState(used, cap);
  const free = used != null && cap ? cap - used : null;
  return (
    <div class="rounded-lg border border-hairline bg-white px-4 py-3.5">
      <div class="flex flex-wrap items-baseline justify-between gap-2.5">
        <span class="font-semibold">{title}</span>
        <span class={`rounded-full px-2 py-0.5 text-xs font-semibold ${STATE_PILL[state]}`}>
          {free == null ? t('state_unknown') : `${free < 0 ? t('meter_over', { kb: kib(-free) }) : t('meter_free', { kb: kib(free) })} · ${t(`state_${state}`)}`}
        </span>
      </div>
      {used != null && cap ? (
        <>
          <div class="mt-1 flex flex-wrap justify-between gap-2.5 font-mono text-sm text-body-secondary tabular-nums">
            <span>{kib(used)} / {fmtKiB(cap)}</span>
            <span>{fmtPct((used / cap) * 100)}</span>
          </div>
          <div class="mt-2.5 h-2 overflow-hidden rounded bg-surface-alt">
            <i class={`block h-full ${STATE_BAR[state]}`} style={{ width: `${Math.min(used / cap, 1) * 100}%` }} />
          </div>
        </>
      ) : null}
    </div>
  );
}
