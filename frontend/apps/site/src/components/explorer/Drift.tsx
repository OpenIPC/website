/**
 * Two builds of one platform, side by side: every package and module whose
 * installed size changed, largest change first.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { Build, Sizes, Source } from '../../lib/explorer/types';
import { fetchSizes } from '../../lib/explorer/api';
import { diffSizes } from '../../lib/explorer/drift';
import { fmtBytes, fmtSignedBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';

interface Props {
  source: Source;
  builds: Build[];
  base: Sizes;
  baseBuild: string;
  compareBuild: string | null;
  platform: string;
  t: ExplorerT;
}

export default function Drift({ source, builds, base, baseBuild, compareBuild, platform, t }: Props) {
  const [other, setOther] = useState<Sizes | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setOther(null);
    setError(null);
    if (!compareBuild) return;
    let live = true;
    fetchSizes(source, compareBuild, platform)
      .then((v) => live && setOther(v))
      .catch((e: Error) => live && setError(e.message));
    return () => { live = false; };
  }, [source, compareBuild, platform]);

  const rows = useMemo(() => (other ? diffSizes(other, base) : []), [other, base]);

  if (!compareBuild || !builds.some((b) => b.id !== baseBuild && b.platforms.includes(platform))) {
    return <p class="text-body-secondary">{t('drift_none_other', { platform })}</p>;
  }
  if (error) return <p class="text-red">{t('error_generic', { error })}</p>;
  if (!other) return <p class="text-body-secondary">{t('loading')}</p>;

  return (
    <>
      <p class="mt-0 mb-3 max-w-[70ch] text-body-secondary">
        {t('drift_intro', { before: compareBuild, after: baseBuild })}
      </p>
      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <th class={TH}>{t('col_kind')}</th>
              <th class={TH}>{t('col_package')}</th>
              <th class={`${TH} text-right`}>{t('col_before')}</th>
              <th class={`${TH} text-right`}>{t('col_after')}</th>
              <th class={`${TH} text-right`}>{t('col_delta')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.kind}:${r.name}`}>
                <td class={`${TD} text-body-secondary`}>{t(`kind_${r.kind}`)}</td>
                <td class={TD}>{r.name}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytes(r.before)}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytes(r.after)}</td>
                <td class={`${TD} ${NUM} font-semibold ${r.delta > 0 ? 'text-red' : 'text-green'}`}>{fmtSignedBytes(r.delta)}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={5} class={`${TD} text-body-secondary`}>{t('no_drift')}</td></tr>}
          </tbody>
        </table>
      </TableBox>
    </>
  );
}
