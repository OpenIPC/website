/**
 * Step one of the shell route: uget, as the printf script its release ships,
 * copied to the clipboard for pasting into telnet. The builds are dynamically
 * linked, so the reader picks by the C library `ls /lib/ld-*` shows; within
 * one library the builds are tried in turn.
 */
import { useState } from 'preact/hooks';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { UGET_BUILDS } from '../../lib/reports';

export default function UgetCopy({ locale, label }: { locale: Locale; label: string }) {
  const t = useBoardsTranslations(locale);
  const [libc, setLibc] = useState<'uclibc' | 'glibc'>('uclibc');
  const builds = UGET_BUILDS.filter((b) => b.libc === libc);
  const [file, setFile] = useState(builds[0].file);
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const chosen = builds.some((b) => b.file === file) ? file : builds[0].file;

  const copy = async () => {
    try {
      const r = await fetch(`/uget/${chosen}`);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      await navigator.clipboard.writeText(await r.text());
      setState('copied');
    } catch {
      setState('failed');
    }
  };

  return (
    <div class="grid gap-2 text-[.9375rem]">
      <label for="uget-libc" class="font-semibold text-ink">{label}</label>
      <div class="flex flex-wrap gap-2">
        <select id="uget-libc" class="max-w-full rounded-md border border-[#cdd3e0] bg-white px-2.5 py-2 text-sm"
          value={libc} onChange={(e) => { setLibc((e.target as HTMLSelectElement).value as 'uclibc' | 'glibc'); setState('idle'); }}>
          <option value="uclibc">{t('report.libc_uclibc')}</option>
          <option value="glibc">{t('report.libc_glibc')}</option>
        </select>
        <select id="uget-build" aria-label={t('report.uget_build')} class="rounded-md border border-[#cdd3e0] bg-white px-2.5 py-2 font-mono text-sm"
          value={chosen} onChange={(e) => { setFile((e.target as HTMLSelectElement).value); setState('idle'); }}>
          {builds.map((b) => <option key={b.file} value={b.file}>{b.toolchain}</option>)}
        </select>
        <button type="button" class="site-btn rounded-md border border-[#cdd3e0] bg-white px-3 py-2 text-sm font-medium text-ink hover:bg-surface-alt" onClick={copy}>
          {t('report.copy_uget')}
        </button>
      </div>
      {state === 'copied' && <p class="m-0 text-sm text-[#1f7a4d]" role="status">{t('report.copied')}</p>}
      {state === 'failed' && (
        <p class="m-0 text-sm" role="status">{t('report.copy_failed')} <a href={`/uget/${chosen}`}>{chosen}</a></p>
      )}
      <p class="m-0 text-[13.5px] text-body-secondary [&_code]:text-[12.5px]" dangerouslySetInnerHTML={{ __html: t('report.uget_hint_html') }} />
    </div>
  );
}
