/**
 * A member's way to send ipctool's report from the camera as their own: a
 * one-time Club code (POST /api/v1/club/reports/code) and the command that
 * carries it, `ipctool upload --note club-XXXX-XXXX` -- the note, because
 * every ipctool in the field can send one. With joins, the report goes with
 * one the member already sent (the photos of the same camera), and the
 * review files both under one board.
 */
import { useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { newReportCode, type ReportCode } from '../../lib/club';

export default function IpctoolCode({ joins, locale, t, label }: {
  joins?: string; locale: Locale; t: BoardsT; label: string;
}) {
  const [code, setCode] = useState<ReportCode | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const ask = () => {
    setBusy(true);
    setError(null);
    newReportCode(joins).then(setCode).catch((e: Error) => setError(e.message)).finally(() => setBusy(false));
  };
  const command = code ? `ipctool upload --note ${code.code}` : '';
  const copy = () => {
    navigator.clipboard?.writeText(command).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }, () => {});
  };

  if (!code) {
    return (
      <span class="grid gap-1">
        <button type="button" class="w-fit cursor-pointer p-0 text-left text-[13px] text-brand-blue underline disabled:opacity-55" disabled={busy} onClick={ask}>
          {label}
        </button>
        {error && <span class="text-[12.5px] text-[#a3262e]" role="alert">{error}</span>}
      </span>
    );
  }
  return (
    <div class="grid gap-1.5 rounded-md border border-hairline bg-surface-alt px-3 py-2.5 text-[13px] text-body">
      <p class="m-0">{t(joins ? 'club.code_joins' : 'club.code_own')}</p>
      <div class="flex flex-wrap items-center gap-2">
        <output class="rounded border border-hairline bg-white px-2 py-1 font-mono text-[13px] text-ink select-all">{command}</output>
        <button type="button" class="site-btn site-btn-outline-primary site-btn-sm" onClick={copy}>{copied ? t('club.add_copied') : t('club.add_copy')}</button>
      </div>
      <p class="m-0 text-[12.5px] text-body-secondary">
        {t('club.add_valid', { time: new Date(code.expires_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' }) })}{' '}
        {t('club.code_stock')} <a href={pathFor(locale, '/cameras/report')}>{t('club.code_stock_link')}</a>
      </p>
    </div>
  );
}
