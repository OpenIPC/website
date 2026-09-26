/**
 * The three tables of one build: packages, kernel modules, and what the
 * finalize hooks removed.
 */
import { Fragment } from 'preact';
import type { ComponentChildren } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import type { SizesModule, SizesPackage, SizesRemoved } from '../../lib/explorer/types';
import { categorise, CATEGORY_COLOUR } from '../../lib/explorer/categorise';
import { fmtBytes, fmtBytesOrNull, fmtNum } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';

export const TABLE = 'w-full border-collapse text-sm';
export const TH = 'whitespace-nowrap border-b border-hairline bg-surface-alt px-3 py-2 text-left text-xs font-semibold tracking-wide text-[#8a93a3] uppercase';
export const TD = 'border-b border-hairline px-3 py-2 align-top';
export const NUM = 'text-right font-mono tabular-nums whitespace-nowrap';

export function TableBox({ children }: { children: ComponentChildren }) {
  return <div class="overflow-x-auto rounded-lg border border-hairline">{children}</div>;
}

type Dir = 'asc' | 'desc';

function useSort<K extends string>(initial: K, textual: readonly K[]) {
  const [key, setKey] = useState<K>(initial);
  const [dir, setDir] = useState<Dir>('desc');
  const toggle = (k: K) => {
    if (k === key) setDir(dir === 'asc' ? 'desc' : 'asc');
    else {
      setKey(k);
      setDir(textual.includes(k) ? 'asc' : 'desc');
    }
  };
  return { key, dir, toggle };
}

function SortTh<K extends string>({ k, sort, class: cls = '', children }: {
  k: K; sort: { key: K; dir: Dir; toggle: (k: K) => void }; class?: string; children: ComponentChildren;
}) {
  const active = sort.key === k;
  return (
    <th class={`${TH} ${cls}`} aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}>
      <button type="button" class="cursor-pointer font-semibold uppercase" onClick={() => sort.toggle(k)}>
        {children}{active ? (sort.dir === 'asc' ? ' ▲' : ' ▼') : ''}
      </button>
    </th>
  );
}

type PkgKey = 'name' | 'uncompressed' | 'compressed' | 'files';

export function PackageTable({ packages, t }: { packages: SizesPackage[]; t: ExplorerT }) {
  const sort = useSort<PkgKey>('uncompressed', ['name']);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const total = useMemo(() => packages.reduce((s, p) => s + p.uncompressed_bytes, 0), [packages]);
  const rows = useMemo(() => {
    const v = (p: SizesPackage) =>
      sort.key === 'uncompressed' ? p.uncompressed_bytes
        : sort.key === 'compressed' ? p.compressed_bytes_approx ?? 0
          : p.file_count ?? 0;
    return [...packages].sort((a, b) => {
      const c = sort.key === 'name' ? a.name.localeCompare(b.name) : v(a) - v(b);
      return sort.dir === 'asc' ? c : -c;
    });
  }, [packages, sort.key, sort.dir]);
  const flip = (name: string) => setOpen((prev) => {
    const next = new Set(prev);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    return next;
  });

  return (
    <>
      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <SortTh k="name" sort={sort}>{t('col_package')}</SortTh>
              <SortTh k="uncompressed" sort={sort} class="text-right">{t('col_installed')}</SortTh>
              <SortTh k="compressed" sort={sort} class="text-right">{t('col_compressed')}</SortTh>
              <SortTh k="files" sort={sort} class="text-right">{t('col_files')}</SortTh>
              <th class={TH}>{t('col_share')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((p) => {
              const share = total ? (p.uncompressed_bytes / total) * 100 : 0;
              const isOpen = open.has(p.name);
              return (
                <Fragment key={p.name}>
                  <tr>
                    <td class={TD}>
                      <button type="button" class="flex cursor-pointer items-center gap-2 text-left" aria-expanded={isOpen} onClick={() => flip(p.name)}>
                        <i class="inline-block size-2.5 shrink-0 rounded-sm" style={{ background: CATEGORY_COLOUR[categorise(p.name)] }} />
                        <span>{isOpen ? '▾' : '▸'} {p.name}</span>
                      </button>
                    </td>
                    <td class={`${TD} ${NUM}`}>{fmtBytes(p.uncompressed_bytes)}</td>
                    <td class={`${TD} ${NUM}`}>{fmtBytesOrNull(p.compressed_bytes_approx)}</td>
                    <td class={`${TD} ${NUM}`}>{fmtNum(p.file_count ?? 0)}</td>
                    <td class={`${TD} whitespace-nowrap`}>
                      <span class="inline-block h-1.5 rounded bg-brand-blue align-middle opacity-75" style={{ width: `${Math.max(1, share * 2.4)}px` }} />{' '}
                      <span class="font-mono text-xs text-body-secondary tabular-nums">{share.toFixed(1)}%</span>
                    </td>
                  </tr>
                  {isOpen && (
                    <tr>
                      <td colSpan={5} class={`${TD} bg-surface-alt/60`}>
                        <TopFiles pkg={p} t={t} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </TableBox>
      <p class="mt-2 text-xs text-body-secondary">{t('compressed_note')}</p>
    </>
  );
}

function TopFiles({ pkg, t }: { pkg: SizesPackage; t: ExplorerT }) {
  const files = pkg.top_files ?? [];
  const total = pkg.file_count ?? files.length;
  if (files.length === 0) return <p class="m-0 text-body-secondary">{t('no_top_files', { total: fmtNum(total) })}</p>;
  const shown = files.reduce((s, f) => s + f.bytes, 0);
  const rest = Math.max(0, total - files.length);
  return (
    <div>
      <p class="mt-0 mb-2 text-body-secondary">
        {t('top_files', { shown: files.length, total: fmtNum(total) })}
        {rest > 0 ? t('top_files_rest', { rest: fmtNum(rest), bytes: fmtBytes(Math.max(0, pkg.uncompressed_bytes - shown)) }) : ''}.
      </p>
      <ul class="m-0 grid list-none gap-1 p-0">
        {files.map((f) => (
          <li key={f.path} class="flex flex-wrap justify-between gap-x-4">
            <code class="font-mono text-[13px] break-all">{f.path}</code>
            <span class="font-mono text-[13px] text-body-secondary tabular-nums">{fmtBytes(f.bytes)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

type ModKey = 'name' | 'bytes' | 'package' | 'autoloaded';
type Filter = 'all' | 'boot' | 'demand';

export function ModuleTable({ modules, t }: { modules: SizesModule[]; t: ExplorerT }) {
  const sort = useSort<ModKey>('bytes', ['name', 'package']);
  const [filter, setFilter] = useState<Filter>('all');
  const rows = useMemo(() => {
    const arr = modules.filter((m) => filter === 'all' || (filter === 'boot' ? m.autoloaded : !m.autoloaded));
    return arr.sort((a, b) => {
      const c = sort.key === 'name' ? a.name.localeCompare(b.name)
        : sort.key === 'package' ? (a.package ?? '').localeCompare(b.package ?? '')
          : sort.key === 'autoloaded' ? Number(a.autoloaded) - Number(b.autoloaded)
            : a.bytes - b.bytes;
      return sort.dir === 'asc' ? c : -c;
    });
  }, [modules, filter, sort.key, sort.dir]);
  const bytes = rows.reduce((s, m) => s + m.bytes, 0);

  return (
    <>
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 text-sm">
          <span class="text-body-secondary">{t('filter_label')}</span>
          <select id="explorer-module-filter" class="rounded-md border border-hairline bg-white px-2.5 py-1.5"
            value={filter} onChange={(e) => setFilter((e.target as HTMLSelectElement).value as Filter)}>
            <option value="all">{t('filter_all')}</option>
            <option value="boot">{t('filter_boot')}</option>
            <option value="demand">{t('filter_demand')}</option>
          </select>
        </label>
        <span class="text-sm text-body-secondary">{t('filter_summary', { count: rows.length, bytes: fmtBytes(bytes) })}</span>
      </div>
      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <SortTh k="name" sort={sort}>{t('col_module')}</SortTh>
              <SortTh k="bytes" sort={sort} class="text-right">{t('col_size')}</SortTh>
              <SortTh k="package" sort={sort}>{t('col_owner')}</SortTh>
              <SortTh k="autoloaded" sort={sort}>{t('col_loaded')}</SortTh>
              <th class={TH}>{t('col_path')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((m) => (
              <tr key={m.path}>
                <td class={`${TD} font-mono text-[13px]`}>{m.name}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytes(m.bytes)}</td>
                <td class={TD}>{m.package ?? '—'}</td>
                <td class={TD}>
                  <span class={`rounded-full px-2 py-0.5 text-xs font-semibold ${m.autoloaded ? 'bg-[#e3f3ea] text-green' : 'bg-surface-alt text-body-secondary'}`}>
                    {m.autoloaded ? t('loaded_boot') : t('loaded_demand')}
                  </span>
                </td>
                <td class={`${TD} font-mono text-[13px] break-all text-body-secondary`}>{m.path}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={5} class={`${TD} text-body-secondary`}>{t('no_modules')}</td></tr>}
          </tbody>
        </table>
      </TableBox>
    </>
  );
}

export function RemovedTable({ removed, t }: { removed: SizesRemoved[]; t: ExplorerT }) {
  const rows = useMemo(() => [...removed].sort((a, b) => (b.source_bytes ?? 0) - (a.source_bytes ?? 0)), [removed]);
  const total = rows.reduce((s, r) => s + (r.source_bytes ?? 0), 0);
  return (
    <>
      <p class="mt-0 mb-2 max-w-[70ch] text-body-secondary">{t('removed_intro')}</p>
      <p class="mt-0 mb-3 font-medium">{t('removed_summary', { count: rows.length, bytes: fmtBytes(total) })}</p>
      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr><th class={TH}>{t('col_path')}</th><th class={TH}>{t('col_owner')}</th><th class={`${TH} text-right`}>{t('col_size')}</th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.path}>
                <td class={`${TD} font-mono text-[13px] break-all`}>{r.path}</td>
                <td class={TD}>{r.package ?? '—'}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytesOrNull(r.source_bytes ?? null)}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={3} class={`${TD} text-body-secondary`}>{t('no_removed')}</td></tr>}
          </tbody>
        </table>
      </TableBox>
    </>
  );
}
