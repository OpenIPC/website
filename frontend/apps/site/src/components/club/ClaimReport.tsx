/**
 * Claim codes (#620). A report sent without signing in -- by ipctool, an AI
 * agent or the send form -- is answered with claim-XXXX-XXXX-XXXX, shown
 * only to its sender; a member enters it on /club (POST
 * /api/v1/club/reports/claim) and the report is theirs, as if sent signed
 * in. ClaimReport is /club's form; SentClaim shows the code a guest's send
 * was given.
 */
import { useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { claimReport } from '../../lib/club';

export default function ClaimReport({ t, onClaimed }: { t: BoardsT; onClaimed: () => void }) {
  const [open, setOpen] = useState(false);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);

  if (!open) {
    return (
      <button type="button" class="w-fit cursor-pointer p-0 text-left text-[13px] text-brand-blue underline" onClick={() => setOpen(true)}>
        {t('club.claim_link')}
      </button>
    );
  }
  const submit = (e: Event) => {
    e.preventDefault();
    if (!code.trim()) return;
    setBusy(true);
    setResult(null);
    claimReport(code)
      .then((c) => {
        setCode('');
        setResult({ ok: true, text: c.points > 0 ? t('club.claim_done_stars', { id: c.id, n: c.points }) : t('club.claim_done', { id: c.id }) });
        onClaimed();
      })
      .catch((err: Error) => setResult({ ok: false, text: t('club.claim_failed', { error: err.message }) }))
      .finally(() => setBusy(false));
  };
  return (
    <form class="grid w-full gap-1.5 rounded-md border border-hairline bg-surface-alt px-3 py-2.5 text-[13px] text-body" onSubmit={submit}>
      <p class="m-0">{t('club.claim_lede')}</p>
      <div class="flex flex-wrap items-center gap-2">
        <input aria-label={t('club.claim_code')} placeholder="claim-XXXX-XXXX-XXXX" value={code} spellcheck={false} autocomplete="off"
          onInput={(e) => setCode((e.target as HTMLInputElement).value)}
          class="w-[17rem] max-w-full rounded border border-hairline bg-white px-2 py-1 font-mono text-[13px]" />
        <button type="submit" class="site-btn site-btn-outline-primary site-btn-sm" disabled={busy || !code.trim()}>{t('club.claim_button')}</button>
      </div>
      {result && <p class={`m-0 text-[12.5px] ${result.ok ? 'text-[#146c3c]' : 'text-[#a3262e]'}`} role={result.ok ? 'status' : 'alert'}>{result.text}</p>}
    </form>
  );
}

export function SentClaim({ code, t }: { code: string; t: BoardsT }) {
  return (
    <div class="grid gap-1 text-body">
      <p class="m-0 text-[12.5px]">{t('club.sent_claim')}</p>
      <output class="w-fit rounded border border-hairline bg-white px-2 py-1 font-mono text-[13px] text-ink select-all">{code}</output>
    </div>
  );
}
