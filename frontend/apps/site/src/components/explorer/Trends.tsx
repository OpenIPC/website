/**
 * The long view of one platform: kernel and root-filesystem use against their
 * caps over the retained builds, when the headroom runs out if the trend
 * holds, and the packages or modules that grew the most.
 */
import { Fragment } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { Source } from '../../lib/explorer/types';
import { fetchTrends, NotFound } from '../../lib/explorer/api';
import { projectOverflow, sortedByDate, topGrowers, type HeadroomPoint, type SeriesPoint, type TrendsFile } from '../../lib/explorer/timeseries';
import { fmtBytes, fmtPerWeek, fmtSignedBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';

const WINDOWS = [7, 14, 30, 90] as const;
const TOP_N = 20;
const DAY = 86_400_000;
const day = (iso: string) => iso.slice(0, 10);

export default function Trends({ source, platform, t }: { source: Source; platform: string; t: ExplorerT }) {
  const [trends, setTrends] = useState<TrendsFile | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const [windowDays, setWindowDays] = useState<number>(90);
  const [kind, setKind] = useState<'packages' | 'modules'>('packages');
  const [minKb, setMinKb] = useState(0);
  const [open, setOpen] = useState<string | null>(null);

  useEffect(() => {
    setTrends(null);
    setError(null);
    setMissing(false);
    fetchTrends(source, platform)
      .then(setTrends)
      .catch((e: Error) => (e instanceof NotFound ? setMissing(true) : setError(e.message)));
  }, [source, platform]);

  const series = trends ? (kind === 'packages' ? trends.packages : trends.modules) : {};
  const growers = useMemo(() => {
    if (!trends) return [];
    const all = topGrowers(series, windowDays, 1000);
    const min = minKb * 1024;
    return (min > 0 ? all.filter((r) => Math.abs(r.perDayBytes * 7) >= min) : all).slice(0, TOP_N);
  }, [trends, series, windowDays, minKb]);

  if (error) return <p class="text-red">{t('error_generic', { error })}</p>;
  if (missing) return <p class="text-body-secondary">{t('trends_none')}</p>;
  if (!trends) return <p class="text-body-secondary">{t('loading')}</p>;
  const hr = trends.headroom_rootfs;
  if (hr.length === 0) return <p class="text-body-secondary">{t('trends_none')}</p>;

  const seg = 'rounded-md border border-hairline px-2.5 py-1 text-sm';
  const on = 'bg-brand-blue text-white border-brand-blue';

  return (
    <div>
      <p class="mt-0 mb-4 text-body-secondary">{t('trends_summary', { count: hr.length, first: day(hr[0].built_at), last: day(hr[hr.length - 1].built_at) })}</p>
      <div class="grid gap-6 lg:grid-cols-2">
        <HeadroomChart title={t('chart_rootfs')} points={trends.headroom_rootfs} windowDays={windowDays} t={t} />
        <HeadroomChart title={t('chart_kernel')} points={trends.headroom_kernel} windowDays={windowDays} t={t} />
      </div>

      <div class="mt-6 mb-3 flex flex-wrap items-end gap-x-5 gap-y-3">
        <div class="flex flex-col gap-1">
          <span class="text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">{t('window_label')}</span>
          <div class="flex gap-1">
            {WINDOWS.map((d) => (
              <button key={d} type="button" aria-pressed={d === windowDays} class={`${seg} ${d === windowDays ? on : 'bg-white'}`} onClick={() => setWindowDays(d)}>
                {t('window_days', { days: d })}
              </button>
            ))}
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <span class="text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">{t('kind_label')}</span>
          <div class="flex gap-1">
            {(['packages', 'modules'] as const).map((k) => (
              <button key={k} type="button" aria-pressed={k === kind} class={`${seg} ${k === kind ? on : 'bg-white'}`} onClick={() => { setKind(k); setOpen(null); }}>
                {t(`kind_${k}`)}
              </button>
            ))}
          </div>
        </div>
        <label class="flex flex-col gap-1">
          <span class="text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">{t('min_weekly')}</span>
          <input id="explorer-min-weekly" type="number" min={0} value={minKb} class="w-24 rounded-md border border-hairline px-2.5 py-1 text-sm"
            onInput={(e) => setMinKb(Number((e.target as HTMLInputElement).value) || 0)} />
        </label>
        <span class="text-sm text-body-secondary">{t('growers_summary', { shown: growers.length, total: Object.keys(series).length })}</span>
      </div>

      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <th class={TH}>{t(kind === 'packages' ? 'col_package' : 'col_module')}</th>
              <th class={`${TH} text-right`}>{t('col_now')}</th>
              <th class={`${TH} text-right`}>{t('col_window_delta')}</th>
              <th class={`${TH} text-right`}>{t('col_per_week')}</th>
              <th class={TH}>{t('col_trend')}</th>
            </tr>
          </thead>
          <tbody>
            {growers.map((g) => {
              const s = sortedByDate(series[g.name] ?? []);
              const isOpen = open === g.name;
              return (
                <Fragment key={g.name}>
                  <tr>
                    <td class={TD}>
                      <button type="button" class="cursor-pointer text-left" aria-expanded={isOpen} onClick={() => setOpen(isOpen ? null : g.name)}>
                        {isOpen ? '▾' : '▸'} {g.name}
                      </button>
                    </td>
                    <td class={`${TD} ${NUM}`}>{fmtBytes(g.lastBytes)}</td>
                    <td class={`${TD} ${NUM} font-semibold ${g.delta > 0 ? 'text-red' : g.delta < 0 ? 'text-green' : ''}`}>{fmtSignedBytes(g.delta)}</td>
                    <td class={`${TD} ${NUM}`}>{fmtPerWeek(g.perDayBytes)}</td>
                    <td class={TD}><Sparkline series={s} windowDays={windowDays} w={120} h={24} t={t} /></td>
                  </tr>
                  {isOpen && (
                    <tr><td colSpan={5} class={`${TD} bg-surface-alt/60`}><Sparkline series={s} windowDays={windowDays} w={640} h={140} axes t={t} /></td></tr>
                  )}
                </Fragment>
              );
            })}
            {growers.length === 0 && (
              <tr><td colSpan={5} class={`${TD} text-body-secondary`}>{t('no_growers', { kb: minKb, days: windowDays })}</td></tr>
            )}
          </tbody>
        </table>
      </TableBox>
    </div>
  );
}

function inWindow<T extends { built_at: string }>(points: readonly T[], windowDays: number): T[] {
  const cutoff = Date.now() - windowDays * DAY;
  return points.filter((p) => new Date(p.built_at).getTime() >= cutoff);
}

function HeadroomChart({ title, points, windowDays, t }: { title: string; points: HeadroomPoint[]; windowDays: number; t: ExplorerT }) {
  const visible = inWindow(points, windowDays).filter((p) => p.used_kb != null && p.cap_kb);
  const proj = useMemo(() => projectOverflow(points, windowDays), [points, windowDays]);
  if (visible.length === 0) return null;

  const W = 640;
  const H = 220;
  const pad = { l: 58, r: 12, t: 14, b: 26 };
  const cap = visible[visible.length - 1].cap_kb;
  const used = visible.map((p) => p.used_kb);
  const lo = Math.max(0, Math.min(...used) - Math.max(16, (cap - Math.min(...used)) * 0.4));
  const hi = Math.max(cap, ...used) + 8;
  const t0 = new Date(visible[0].built_at).getTime();
  const span = Math.max(1, new Date(visible[visible.length - 1].built_at).getTime() - t0);
  const x = (iso: string) => pad.l + ((new Date(iso).getTime() - t0) / span) * (W - pad.l - pad.r);
  const y = (kb: number) => pad.t + ((hi - kb) / (hi - lo)) * (H - pad.t - pad.b);
  const path = visible.map((p, i) => `${i ? 'L' : 'M'}${x(p.built_at).toFixed(1)} ${y(p.used_kb).toFixed(1)}`).join(' ');
  const last = visible[visible.length - 1];
  const ticks = [Math.round(lo), Math.round((lo + cap) / 2)];

  return (
    <div class="min-w-0">
      <div class="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h3 class="m-0 text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">{title}</h3>
        {proj && proj.daysToZero < 365 && (
          <span class="rounded-full bg-[#fbe4e6] px-2 py-0.5 text-xs font-semibold text-red">
            {t('projection', { days: Math.round(proj.daysToZero), date: day(proj.projectedDate) })}
          </span>
        )}
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} class="block h-auto w-full" role="img"
        aria-label={`${title}: ${last.used_kb} / ${last.cap_kb} KiB`}>
        {ticks.map((v) => (
          <g key={v}>
            <line x1={pad.l} x2={W - pad.r} y1={y(v)} y2={y(v)} stroke="#e3e7f0" />
            <text x={pad.l - 8} y={y(v) + 4} text-anchor="end" class="fill-[#8a93a3] font-mono text-[12px]">{v.toLocaleString('en')}</text>
          </g>
        ))}
        <line x1={pad.l} x2={W - pad.r} y1={y(cap)} y2={y(cap)} stroke="#d92839" stroke-dasharray="4 4" />
        <text x={W - pad.r} y={y(cap) - 6} text-anchor="end" class="fill-red font-mono text-[12px]">{t('chart_cap', { kb: cap.toLocaleString('en') })}</text>
        <path d={`${path} L${x(last.built_at)} ${H - pad.b} L${x(visible[0].built_at)} ${H - pad.b}Z`} fill="#4c60d8" fill-opacity="0.12" />
        <path d={path} fill="none" stroke="#4c60d8" stroke-width="2" />
        {visible.map((p) => (
          <circle key={p.build_id} cx={x(p.built_at)} cy={y(p.used_kb)} r={p === last ? 4 : 0} fill="#4c60d8">
            <title>{p.build_id} · {p.used_kb.toLocaleString('en')} / {p.cap_kb.toLocaleString('en')} KiB</title>
          </circle>
        ))}
        <text x={pad.l} y={H - 8} class="fill-[#8a93a3] font-mono text-[12px]">{day(visible[0].built_at)}</text>
        <text x={W - pad.r} y={H - 8} text-anchor="end" class="fill-[#8a93a3] font-mono text-[12px]">{day(last.built_at)}</text>
      </svg>
    </div>
  );
}

function Sparkline({ series, windowDays, w, h, axes = false, t }: {
  series: SeriesPoint[]; windowDays: number; w: number; h: number; axes?: boolean; t: ExplorerT;
}) {
  const v = inWindow(series, windowDays);
  if (v.length < 2) return <span class="text-xs text-body-secondary">{t('only_points', { count: v.length })}</span>;
  const min = Math.min(...v.map((p) => p.bytes));
  const max = Math.max(...v.map((p) => p.bytes));
  const t0 = new Date(v[0].built_at).getTime();
  const span = Math.max(1, new Date(v[v.length - 1].built_at).getTime() - t0);
  const pad = axes ? 30 : 2;
  const pts = v.map((p) => [
    pad + ((new Date(p.built_at).getTime() - t0) / span) * (w - pad * 2),
    pad + (h - pad * 2) - ((p.bytes - min) / Math.max(1, max - min)) * (h - pad * 2),
  ]);
  const colour = v[v.length - 1].bytes > v[0].bytes ? '#d92839' : v[v.length - 1].bytes < v[0].bytes ? '#138350' : '#8a93a3';
  return (
    <svg viewBox={`0 0 ${w} ${h}`} width={axes ? undefined : w} height={axes ? undefined : h} class={axes ? 'block h-auto w-full' : 'block'} aria-hidden="true">
      {axes && (
        <>
          <text x={4} y={pad + 4} class="fill-[#8a93a3] font-mono text-[11px]">{fmtBytes(max)}</text>
          <text x={4} y={h - pad} class="fill-[#8a93a3] font-mono text-[11px]">{fmtBytes(min)}</text>
          <text x={pad} y={h - 6} class="fill-[#8a93a3] font-mono text-[11px]">{day(v[0].built_at)}</text>
          <text x={w - pad} y={h - 6} text-anchor="end" class="fill-[#8a93a3] font-mono text-[11px]">{day(v[v.length - 1].built_at)}</text>
        </>
      )}
      <path d={pts.map(([px, py], i) => `${i ? 'L' : 'M'}${px.toFixed(1)} ${py.toFixed(1)}`).join(' ')} fill="none" stroke={colour} stroke-width="1.5" />
    </svg>
  );
}
