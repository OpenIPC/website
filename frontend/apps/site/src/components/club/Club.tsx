/**
 * /club: signing in, and once in, the member's own page -- their stars and
 * every report they sent, its state and what it earned (service/internal/club).
 *
 * The page is static; who is signed in comes from /api/v1/club/me in the
 * browser. Telegram signs in through the bot: this browser shows a code (a
 * QR code on a desktop), the person taps Start in Telegram, and this page,
 * polling, finds itself signed in.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import { QrCodeWidget } from '@openipc/ui';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import {
  clock, fetchCameras, fetchMe, fetchMine, finishConfirm, finishWho, pollLogin, rename, secondsLeft, setListed, setQuiet, signOut,
  startEmail, startTelegram, STARS, type Cameras as CameraData, type Me, type Member, type MemberReport,
} from '../../lib/club';
import { size } from '../../lib/reports';
import Cameras from './Cameras';
import IpctoolCode from './IpctoolCode';
import { STATUS_TONE, Stars } from './parts';

type Load = { state: 'loading' } | { state: 'ok'; me: Me } | { state: 'error' };

export default function Club({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [notice, setNotice] = useState<string | null>(null);
  // A sign-in link opened in a browser other than the one that asked for
  // it: whose account it is for, asked before anything is signed in.
  const [confirm, setConfirm] = useState<{ code: string; who: string } | null>(null);

  const reload = () => fetchMe().then((me) => setLoad({ state: 'ok', me })).catch(() => setLoad({ state: 'error' }));

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const q = params.get('signin');
    const code = params.get('confirm');
    if (q && q !== 'ok') setNotice(t(`club.signin_${q === 'unavailable' ? 'failed' : q}`, { fallback: t('club.signin_failed') }));
    if (code) {
      finishWho(code)
        .then((r) => setConfirm({ code, who: r.who }))
        .catch(() => setNotice(t('club.signin_expired')));
    }
    if (q || code) history.replaceState(null, '', location.pathname);
    reload();
  }, []);

  if (load.state === 'loading') return <div class="mt-8 h-48 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />;
  if (load.state === 'error') return <p class="mt-8 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>;
  const { me } = load;
  return (
    <>
      {notice && <p class="mt-6 mb-0 rounded-md bg-[#fff4e2] px-3 py-2 text-sm text-[#9a5b00]" role="alert">{notice}</p>}
      {confirm && (
        <section class="mt-6 grid max-w-[640px] gap-3 rounded-xl border border-[#e8c58f] bg-[#fff4e2] p-5" aria-labelledby="club-confirm">
          <h2 id="club-confirm" class="m-0 text-lg font-semibold">{t('club.confirm_title', { who: confirm.who })}</h2>
          <p class="m-0 text-sm text-[#7a4a00]">{t('club.confirm_text', { who: confirm.who })}</p>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="site-btn site-btn-primary" onClick={() => finishConfirm(confirm.code)
              .then(() => { setConfirm(null); reload(); })
              .catch(() => { setConfirm(null); setNotice(t('club.signin_expired')); })}>
              {t('club.confirm_button')}
            </button>
            <button type="button" class="site-btn site-btn-outline-secondary" onClick={() => setConfirm(null)}>{t('club.confirm_cancel')}</button>
          </div>
        </section>
      )}
      {me.member
        ? <MemberPage member={me.member} ways={me.sign_in} locale={locale} t={t} onChange={reload} />
        : <SignIn ways={me.sign_in} locale={locale} t={t} onSignedIn={reload} />}
    </>
  );
}

function SignIn({ ways, locale, t, onSignedIn, compact }: {
  ways: Me['sign_in']; locale: Locale; t: BoardsT; onSignedIn: () => void; compact?: boolean;
}) {
  const [sheet, setSheet] = useState(false);
  const [email, setEmail] = useState('');
  const [mail, setMail] = useState<{ state: 'idle' | 'sending' } | { state: 'sent'; to: string } | { state: 'error'; error: string }>({ state: 'idle' });

  const sendLink = (e: Event) => {
    e.preventDefault();
    setMail({ state: 'sending' });
    startEmail(email, locale)
      .then(() => setMail({ state: 'sent', to: email }))
      .catch((err: Error) => setMail({ state: 'error', error: err.message }));
  };

  const provider = 'flex w-full items-center gap-3 rounded-lg border border-hairline bg-white px-3.5 py-2.5 text-left font-semibold text-ink enabled:hover:border-brand-blue disabled:cursor-not-allowed disabled:opacity-55';
  const mark = 'grid size-[30px] shrink-0 place-items-center rounded-[7px] font-mono text-[13px] font-bold text-white';
  const buttons = (
    <div class="grid content-start gap-2.5">
      <button type="button" class={provider} disabled={!ways.telegram} onClick={() => setSheet(true)}>
        <span class={`${mark} bg-[#2aabee]`}>TG</span>
        <span>{t('club.tg_button')}<small class="block text-[12.5px] font-normal text-body-secondary">{ways.telegram ? t('club.tg_hint') : t('club.unavailable')}</small></span>
      </button>
      {ways.github
        ? (
          <a class={`${provider} no-underline`} href="/api/v1/club/github">
            <span class={`${mark} bg-ink-2`}>GH</span>
            <span>{t('club.gh_button')}<small class="block text-[12.5px] font-normal text-body-secondary">{t('club.gh_hint')}</small></span>
          </a>
        )
        : (
          <button type="button" class={provider} disabled>
            <span class={`${mark} bg-ink-2`}>GH</span>
            <span>{t('club.gh_button')}<small class="block text-[12.5px] font-normal text-body-secondary">{t('club.unavailable')}</small></span>
          </button>
        )}
      <div class="flex items-center gap-2.5 text-[12.5px] text-body-secondary before:h-px before:flex-1 before:bg-hairline after:h-px after:flex-1 after:bg-hairline">{t('club.or')}</div>
      {mail.state === 'sent'
        ? <p class="m-0 rounded-md bg-[#e7f5ee] px-3 py-2 text-sm text-[#1f7a4d]" role="status">{t('club.email_sent', { email: mail.to })}</p>
        : (
          <form class="flex flex-wrap gap-2" onSubmit={sendLink}>
            <label for="club-email" class="basis-full text-[12.5px] text-body-secondary">{ways.email ? t('club.email_label') : t('club.unavailable')}</label>
            <input id="club-email" type="email" required autocomplete="email" placeholder={t('club.email_placeholder')} value={email}
              disabled={!ways.email} onInput={(e) => setEmail((e.target as HTMLInputElement).value)}
              class="min-w-0 flex-[1_1_200px] rounded-md border border-hairline px-2.5 py-2 text-[15px]" />
            <button type="submit" class="site-btn site-btn-primary disabled:cursor-not-allowed disabled:opacity-55" disabled={!ways.email || mail.state === 'sending'}>{t('club.email_button')}</button>
            {mail.state === 'error' && <p class="m-0 basis-full text-sm text-[#a3262e]" role="alert">{mail.error}</p>}
          </form>
        )}
    </div>
  );

  return (
    <>
      {compact
        ? buttons
        : (
          <section class="mt-8 grid overflow-hidden rounded-xl border border-hairline md:grid-cols-[1fr_1.1fr]" aria-labelledby="club-why">
            <div class="grid content-start gap-3 bg-surface-alt p-6">
              <h2 id="club-why" class="m-0 text-lg font-semibold">{t('club.why_title')}</h2>
              <ul class="m-0 grid list-disc gap-1.5 ps-5 text-[.9375rem]">
                <li>{t('club.why_1')}</li><li>{t('club.why_2')}</li><li>{t('club.why_3')}</li>
              </ul>
              <p class="m-0 text-[12.5px] text-body-secondary">{t('club.why_note')}</p>
            </div>
            <div class="p-6">{buttons}</div>
          </section>
        )}
      {sheet && ways.telegram && <TelegramSheet bot={ways.telegram} t={t} onClose={() => setSheet(false)} onSignedIn={() => { setSheet(false); onSignedIn(); }} />}
      {!compact && <Rules t={t} />}
    </>
  );
}

/**
 * The QR code, the steps, and a countdown, while the page polls every two
 * seconds for the Start tap. A phone gets the link to open straight away.
 */
function TelegramSheet({ bot, t, onClose, onSignedIn }: { bot: string; t: BoardsT; onClose: () => void; onSignedIn: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [link, setLink] = useState<{ url: string; expires: string } | null>(null);
  const [left, setLeft] = useState(600);
  const [error, setError] = useState<string | null>(null);
  const [round, setRound] = useState(0);

  useEffect(() => { dialog.current?.showModal(); }, []);
  useEffect(() => {
    let live = true;
    setLink(null);
    setError(null);
    startTelegram()
      .then((r) => {
        if (!live) return;
        setLink({ url: r.link, expires: r.expires_at });
        // A phone has Telegram on it: go there now rather than show a code
        // to scan with the same phone.
        if (matchMedia('(pointer: coarse)').matches) location.href = r.link;
      })
      .catch((e: Error) => live && setError(e.message));
    return () => { live = false; };
  }, [round]);
  useEffect(() => {
    if (!link) return;
    let live = true;
    const tick = setInterval(() => setLeft(secondsLeft(link.expires)), 1000);
    const poll = async () => {
      while (live) {
        await new Promise((r) => setTimeout(r, 2000));
        if (!live || document.hidden) continue;
        try {
          const p = await pollLogin();
          if (p.state === 'signed_in') { onSignedIn(); return; }
          if (p.state === 'expired' || p.state === 'none') { setLeft(0); return; }
        } catch { /* a dropped request; the next one tries again */ }
      }
    };
    void poll();
    return () => { live = false; clearInterval(tick); };
  }, [link]);

  return (
    <dialog ref={dialog} aria-labelledby="tg-sheet" onClose={onClose}
      class="m-auto w-[min(560px,calc(100vw-1rem))] max-w-none overflow-hidden rounded-xl border border-hairline bg-white p-0 text-body shadow-2xl backdrop:bg-ink/70">
      <div class="flex items-center justify-between border-b border-hairline px-5 py-3.5">
        <h2 id="tg-sheet" class="m-0 text-lg font-semibold">{t('club.tg_title')}</h2>
        <button type="button" aria-label={t('club.close')} onClick={() => dialog.current?.close()} class="cursor-pointer px-2 text-xl leading-none text-body-secondary hover:text-body">✕</button>
      </div>
      <div class="grid gap-5 p-5 sm:grid-cols-[172px_1fr]">
        {/* The shared widget frames its code in grey for the Wi-Fi page; here
            the tile is the frame, so the code fills it. */}
        <div class="aspect-square w-[172px] max-w-full rounded-[10px] border border-hairline bg-white p-2 [&_svg]:!bg-white [&_svg]:!p-0">
          {link && left > 0
            ? <QrCodeWidget textToCode={link.url} />
            : <div class="grid h-full place-items-center text-center text-sm text-body-secondary">{error ?? (left === 0 ? t('club.tg_expired') : '…')}</div>}
        </div>
        <ol class="m-0 grid list-decimal content-start gap-2 ps-5 text-[.9375rem]">
          <li>{t('club.tg_step1')}</li>
          <li>{t('club.tg_step2', { bot: `@${bot}` })}</li>
          <li>{t('club.tg_step3')}</li>
        </ol>
      </div>
      <div class="flex flex-wrap gap-2 px-5 pb-5">
        {link && left > 0 && <a class="site-btn site-btn-primary !border-[#2aabee] !bg-[#2aabee]" href={link.url}>{t('club.tg_open')}</a>}
        {left === 0 && <button type="button" class="site-btn site-btn-primary" onClick={() => { setLeft(600); setRound(round + 1); }}>{t('club.tg_retry')}</button>}
        <button type="button" class="site-btn site-btn-outline-primary" onClick={() => dialog.current?.close()}>{t('club.tg_other')}</button>
      </div>
      {link && left > 0 && (
        <p class="m-0 flex items-center gap-2.5 border-t border-hairline bg-surface-alt px-5 py-3 text-[13.5px] text-body-secondary" role="status">
          <span class="size-2.5 shrink-0 animate-pulse rounded-full bg-[#2aabee]" aria-hidden="true" />
          {t('club.tg_waiting', { time: clock(left) })}
        </p>
      )}
    </dialog>
  );
}

function MemberPage({ member, ways, locale, t, onChange }: {
  member: Member; ways: Me['sign_in']; locale: Locale; t: BoardsT; onChange: () => void;
}) {
  const [reports, setReports] = useState<MemberReport[] | null>(null);
  const [cams, setCams] = useState<CameraData | null>(null);
  const [error, setError] = useState(false);
  const [linking, setLinking] = useState(false);
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState(member.name);
  const [nameError, setNameError] = useState<string | null>(null);
  // What waits for review comes with the reports, not with /me: the navbar
  // asks /me on every page and needs only the total.
  const pending = (reports ?? []).reduce((n, r) => n + (r.status === 'pending' ? r.pending : 0), 0);

  useEffect(() => {
    fetchMine().then((r) => setReports(r.reports)).catch(() => setError(true));
  }, [member.id]);
  const loadCams = () => fetchCameras().then(setCams).catch(() => setError(true));
  useEffect(() => { void loadCams(); }, [member.id]);
  // A link to #cameras (the bot's "My cameras") lands on the section once it exists.
  useEffect(() => {
    if (cams && location.hash === '#cameras') document.getElementById('cameras')?.scrollIntoView();
  }, [cams !== null]);

  const telegram = member.identities.some((i) => i.provider === 'telegram' && i.chat);
  const missing = (['telegram', 'github', 'email'] as const).filter((p) => !member.identities.some((i) => i.provider === p));
  const initials = member.name.split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase();

  return (
    <section class="mt-8 grid overflow-hidden rounded-xl border border-hairline md:grid-cols-[270px_1fr]" aria-labelledby="club-mine">
      <aside class="grid content-start gap-4 border-b border-hairline bg-surface-alt p-5 md:border-e md:border-b-0">
        <div class="flex items-center gap-3">
          <span class="grid size-11 place-items-center rounded-full bg-brand-blue font-semibold text-white" aria-hidden="true">{initials}</span>
          <div>
            <b class="text-ink">{member.name}</b>
            <div class="text-[12.5px] text-body-secondary">{member.identities.map((i) => t(`club.provider_${i.provider}`)).join(' · ')}</div>
          </div>
        </div>
        <div>
          <Stars n={member.stars} big />
          {pending > 0 && <div class="text-[13.5px] text-body-secondary">{t('club.pending', { n: pending })}</div>}
          <dl class="m-0 mt-2 grid gap-0.5 text-[13px]">
            <div class="flex justify-between gap-2"><dt class="text-body-secondary">{t('club.side_from_reports')}</dt><dd class="m-0"><Stars n={member.report_stars ?? member.stars} /></dd></div>
            <div class="flex justify-between gap-2"><dt class="text-body-secondary">{t('club.side_from_wall')}</dt><dd class="m-0"><Stars n={member.wall_stars ?? 0} /></dd></div>
          </dl>
        </div>
        {cams && (
          <label class="flex cursor-pointer items-start gap-2 text-[13px]">
            <input type="checkbox" class="mt-0.5 accent-brand-blue" checked={cams.listed}
              onChange={(e) => {
                const listed = (e.target as HTMLInputElement).checked;
                setCams({ ...cams, listed });
                setListed(listed).catch(() => setCams({ ...cams, listed: !listed }));
              }} />
            <span>{t('club.listed')} · <a href={pathFor(locale, '/club/leaderboard')}>{t('club.leaderboard_link')}</a></span>
          </label>
        )}
        {naming
          ? (
            <form class="grid gap-1.5" onSubmit={(e) => {
              e.preventDefault();
              setNameError(null);
              rename(name).then(() => { setNaming(false); onChange(); }).catch((err: Error) => setNameError(err.message));
            }}>
              <label for="club-name" class="text-[12.5px] text-body-secondary">{t('club.rename_label')}</label>
              <div class="flex gap-1.5">
                <input id="club-name" value={name} maxLength={80} onInput={(e) => setName((e.target as HTMLInputElement).value)}
                  class="min-w-0 flex-1 rounded-md border border-hairline px-2 py-1 text-sm" />
                <button type="submit" class="site-btn site-btn-primary site-btn-sm">{t('club.rename_save')}</button>
              </div>
              {nameError && <span class="text-[12.5px] text-[#a3262e]" role="alert">{nameError}</span>}
            </form>
          )
          : <button type="button" class="w-fit cursor-pointer p-0 text-[13px] text-brand-blue underline" onClick={() => setNaming(true)}>{t('club.rename')}</button>}
        {member.maintainer && <a class="site-btn site-btn-dark site-btn-sm w-fit" href={pathFor(locale, '/club/review')}>{t('club.review_link')}</a>}
        {telegram && (
          <div class="grid gap-1.5 text-[13px] text-body-secondary">
            <span>{member.quiet ? t('club.bot_muted') : t('club.bot_on')}</span>
            <button type="button" class="w-fit cursor-pointer p-0 text-brand-blue underline" onClick={() => setQuiet(!member.quiet).then(onChange)}>
              {member.quiet ? t('club.unmute') : t('club.mute')}
            </button>
          </div>
        )}
        {missing.length > 0 && (
          linking
            ? <SignIn ways={{ telegram: missing.includes('telegram') ? ways.telegram : null, github: missing.includes('github') && ways.github, email: missing.includes('email') && ways.email }} locale={locale} t={t} onSignedIn={onChange} compact />
            : <button type="button" class="w-fit cursor-pointer p-0 text-[13px] text-brand-blue underline" onClick={() => setLinking(true)}>{t('club.link_more')}</button>
        )}
        <button type="button" class="site-btn site-btn-outline-secondary site-btn-sm w-fit" onClick={() => signOut().then(onChange)}>{t('club.sign_out')}</button>
      </aside>
      <div class="min-w-0">
        {cams
          ? <Cameras data={cams} locale={locale} t={t} onChange={() => { void loadCams(); onChange(); }} />
          : !error && <div class="m-4 h-24 animate-pulse rounded bg-surface-alt" />}
        <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-hairline px-4 py-3">
          <h2 id="club-mine" class="m-0 text-lg font-semibold">{t('club.mine_title')}</h2>
          <span class="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-sm">
            <a href={`${pathFor(locale, '/cameras/report')}#new`}>{t('club.new_link')}</a>
            <IpctoolCode locale={locale} t={t} label={t('club.code_get')} />
          </span>
        </div>
        {error && <p class="m-4 text-sm text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
        {!error && reports === null && <div class="m-4 h-24 animate-pulse rounded bg-surface-alt" />}
        {reports && reports.length === 0 && (
          <p class="m-4 text-body-secondary">{t('club.mine_empty')} <a href={pathFor(locale, '/cameras/boards')}>{t('club.boards_link')}</a></p>
        )}
        {reports && reports.length > 0 && <Ledger reports={reports} locale={locale} t={t} />}
        <div class="border-t border-hairline px-4 py-4"><Rules t={t} /></div>
      </div>
    </section>
  );
}

function Ledger({ reports, locale, t }: { reports: MemberReport[]; locale: Locale; t: BoardsT }) {
  return (
    <div class="overflow-x-auto">
      <table class="w-full min-w-[520px] border-collapse text-sm">
        <thead>
          <tr class="text-left font-mono text-[11px] tracking-wide text-body-secondary uppercase">
            <th class="px-4 py-2.5 font-medium">{t('club.col_submission')}</th>
            <th class="px-4 py-2.5 font-medium">{t('club.col_status')}</th>
            <th class="px-4 py-2.5 text-right font-medium">{t('club.col_stars')}</th>
          </tr>
        </thead>
        <tbody>
          {reports.map((r) => {
            const shown = r.status === 'pending' ? r.pending : r.stars;
            const board = r.board ? `${r.board.manufacturer} ${r.board.model}`
              : r.proposal ? t('club.proposed', { board: `${r.proposal.maker} ${r.proposal.board}` })
              : (r.chip || t('club.no_board'));
            return (
              <tr key={r.id} class="border-t border-hairline align-top">
                <td class="grid gap-1 px-4 py-3">
                  <span>
                    {r.board ? <a href={`${pathFor(locale, '/cameras/boards')}?model=${encodeURIComponent(r.board.id)}`}>{board}</a> : board}
                    <span class="ms-2 font-mono text-[12px] text-body-secondary">{r.id}</span>
                    {r.joins && <span class="ms-2 text-[12px] text-body-secondary">{t('club.goes_with', { id: r.joins })}</span>}
                  </span>
                  <ul class="m-0 grid list-none gap-0.5 p-0 text-[12.5px] text-body-secondary">
                    {r.files.map((f) => (
                      <li key={f.position} class="flex flex-wrap items-center gap-x-2">
                        <a href={f.url} download={f.name}>{t(`club.kind_${f.kind}`)} · {f.name}</a>
                        <span class="tabular-nums">{size(f.bytes)}</span>
                        {f.private && <span class="rounded-full bg-[#eef0fc] px-2 text-[11.5px] font-semibold text-[#3d4dad]">{t('club.private')}</span>}
                      </li>
                    ))}
                  </ul>
                  {r.duplicate && <span class="text-[12.5px] text-body-secondary">{t('club.duplicate')}</span>}
                  {r.status !== 'rejected' && !r.joins && <IpctoolCode joins={r.id} locale={locale} t={t} label={t('club.code_add_ipctool')} />}
                  {r.review_note && (
                    <span class="text-[12.5px] text-body"><b class="font-semibold">{t('club.review_note_label')}:</b> {r.review_note}</span>
                  )}
                </td>
                <td class="px-4 py-3"><span class={`whitespace-nowrap rounded-full px-2.5 py-0.5 text-[12.5px] font-semibold ${STATUS_TONE[r.status]}`}>{t(`club.status_${r.status}`)}</span></td>
                <td class={`px-4 py-3 text-right font-semibold whitespace-nowrap tabular-nums ${r.status === 'pending' ? 'font-normal text-body-secondary' : shown > 0 ? 'text-[#9a5b00]' : 'text-body-secondary'}`}>
                  {shown > 0 ? `+${shown} ★` : '0'}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function Rules({ t }: { t: BoardsT }) {
  return (
    <section class="mt-6 grid max-w-[640px] gap-2 text-sm" aria-labelledby="club-rules">
      <h2 id="club-rules" class="m-0 text-base font-semibold">{t('club.rules_title')}</h2>
      <table class="w-full border-collapse">
        <tbody>
          {([
            ['rules_item', `+${STARS.item} ★`], ['rules_dump', `+${STARS.dump} ★`],
            ['rules_join', `+${STARS.join} ★`], ['rules_month', `+${STARS.month} ★`], ['rules_rare', `+${STARS.rare} ★`],
            ['rules_none', '0'],
          ] as const).map(([k, v]) => (
            <tr key={k} class="border-b border-hairline">
              <td class="py-1.5">{t(`club.${k}`)}</td>
              <td class={`py-1.5 text-right font-semibold whitespace-nowrap tabular-nums ${v === '0' ? 'text-body-secondary' : 'text-[#9a5b00]'}`}>{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p class="m-0 text-[12.5px] text-body-secondary">{t('club.rules_note')}</p>
    </section>
  );
}

export { SignIn };
