/**
 * A maintainer's tokens for their own tools: the agents and the analysis on
 * their machine read the flash dumps owners sent at /api/v1/maint/dumps
 * with one, instead of a browser session. A dump is sent as it was sent;
 * every dump asked for is written down. The token is shown once, when it is made.
 */
import { useEffect, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { fetchTokens, mintToken, revokeToken, type ClubToken } from '../../lib/club';

export default function Tokens({ locale, t }: { locale: Locale; t: BoardsT }) {
  const [tokens, setTokens] = useState<ClubToken[] | null>(null);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [days, setDays] = useState(90);
  const [cidr, setCidr] = useState('');
  const [correct, setCorrect] = useState(false);
  const [fresh, setFresh] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = () => fetchTokens().then(setTokens).catch((e: Error) => setError(e.message));
  useEffect(() => { if (open) void load(); }, [open]);
  const date = (s: string) => new Date(s).toLocaleDateString(locale, { dateStyle: 'medium' });
  const live = (k: ClubToken) => !k.revoked_at && new Date(k.expires_at) > new Date();

  if (!open) {
    return (
      <button type="button" class="w-fit cursor-pointer p-0 text-left text-[13px] text-brand-blue underline" onClick={() => setOpen(true)}>
        {t('club.tokens_link')}
      </button>
    );
  }
  return (
    <div class="grid gap-2 rounded-md border border-hairline bg-surface-alt px-3 py-2.5 text-[13px] text-body">
      <p class="m-0">{t('club.tokens_about')}</p>
      {fresh && (
        <div class="grid gap-1.5" role="status">
          <p class="m-0 font-semibold">{t('club.tokens_once')}</p>
          <div class="flex flex-wrap items-center gap-2">
            <output class="break-all rounded border border-hairline bg-white px-2 py-1 font-mono text-[12px] text-ink select-all">{fresh}</output>
            <button type="button" class="site-btn site-btn-outline-primary site-btn-sm" onClick={() => {
              navigator.clipboard?.writeText(fresh).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }, () => {});
            }}>{copied ? t('club.add_copied') : t('club.add_copy')}</button>
          </div>
        </div>
      )}
      <form class="grid gap-1.5" onSubmit={(e) => {
        e.preventDefault();
        setError(null);
        mintToken(name, days, cidr, correct).then((r) => { setFresh(r.token); setName(''); setCidr(''); setCorrect(false); return load(); })
          .catch((err: Error) => setError(err.message));
      }}>
        <label class="grid gap-0.5">
          <span class="text-body-secondary">{t('club.tokens_name')}</span>
          <input required maxLength={100} value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)}
            class="min-w-0 rounded-md border border-hairline px-2 py-1 text-sm" />
        </label>
        <div class="flex flex-wrap gap-2">
          <label class="grid gap-0.5">
            <span class="text-body-secondary">{t('club.tokens_days')}</span>
            <input type="number" min={1} max={366} value={days} onInput={(e) => setDays(Number((e.target as HTMLInputElement).value))}
              class="w-24 rounded-md border border-hairline px-2 py-1 text-sm" />
          </label>
          <label class="grid min-w-0 flex-1 gap-0.5">
            <span class="text-body-secondary">{t('club.tokens_cidr')}</span>
            <input value={cidr} placeholder="203.0.113.7" onInput={(e) => setCidr((e.target as HTMLInputElement).value)}
              class="min-w-0 rounded-md border border-hairline px-2 py-1 text-sm" />
          </label>
        </div>
        <label class="flex items-start gap-1.5">
          <input type="checkbox" checked={correct} onChange={(e) => setCorrect((e.target as HTMLInputElement).checked)} class="mt-0.5" />
          <span>{t('club.tokens_correct')}</span>
        </label>
        <button type="submit" class="site-btn site-btn-primary site-btn-sm w-fit">{t('club.tokens_mint')}</button>
      </form>
      {error && <span class="text-[12.5px] text-[#a3262e]" role="alert">{error}</span>}
      {tokens && tokens.length > 0 && (
        <ul class="m-0 grid list-none gap-1 p-0">
          {tokens.map((k) => (
            <li key={k.id} class="flex flex-wrap items-baseline justify-between gap-x-2 border-t border-hairline pt-1">
              <span>
                <span class={live(k) ? 'font-medium' : 'text-body-secondary line-through'}>{k.name}</span>{' '}
                <span class="text-[12px] text-body-secondary">
                  {t('club.tokens_until', { date: date(k.expires_at) })}
                  {k.scope.includes('reports:correct') && ` · ${t('club.tokens_corrects')}`}
                  {k.allowed_cidr && ` · ${k.allowed_cidr}`}
                  {' · '}{k.last_used_at ? t('club.tokens_used', { date: date(k.last_used_at) }) : t('club.tokens_unused')}
                </span>
              </span>
              {live(k) && (
                <button type="button" class="cursor-pointer p-0 text-[12.5px] text-[#a3262e] underline"
                  onClick={() => { revokeToken(k.id).then(load).catch((e: Error) => setError(e.message)); }}>
                  {t('club.tokens_revoke')}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
