/**
 * A camera the catalogue does not have: its maker, its board's marking and
 * photos of it are a report (POST /api/v1/club/reports, channel web, maker,
 * board and soc instead of a board's model). A whole-flash dump -- read
 * with a programmer when ipctool cannot run, or ipctool's backup -- is
 * private unless the sender ticks the box, as on the board pages; ipctool's
 * output and a boot log help the reviewer. All of them are optional. A maintainer who publishes the report
 * adds the board to the catalogue, and the photos become its first unit.
 *
 * Signing in is not required, as with SendForm; stars are a member's.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { STARS, fetchMe, remembered, sendReport, type Member, type Sent } from '../../lib/club';

const field = 'rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body';
const area = 'min-h-[72px] w-full resize-y rounded-md border border-hairline bg-surface-alt p-2 font-mono text-[12.5px] leading-snug text-body';

export default function NewCameraForm({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [maker, setMaker] = useState('');
  const [board, setBoard] = useState('');
  const [soc, setSoc] = useState('');
  const [photos, setPhotos] = useState<File[]>([]);
  const [dump, setDump] = useState<File | null>(null);
  const [publicDump, setPublicDump] = useState(false);
  const [ipctool, setIpctool] = useState('');
  const [bootLog, setBootLog] = useState('');
  const [note, setNote] = useState('');
  const [member, setMember] = useState<Member | null | undefined>(() => (remembered() ? undefined : null));
  const [state, setState] = useState<{ s: 'idle' | 'sending' } | { s: 'sent'; sent: Sent } | { s: 'error'; error: string }>({ s: 'idle' });

  useEffect(() => {
    if (remembered()) fetchMe().then((me) => setMember(me.member)).catch(() => setMember(null));
  }, []);

  const submit = (e: Event) => {
    e.preventDefault();
    if (!maker.trim() || !board.trim() || photos.length === 0) {
      setState({ s: 'error', error: t('club.new_need') });
      return;
    }
    const form = new FormData();
    form.set('channel', 'web');
    form.set('maker', maker.trim());
    form.set('board', board.trim());
    if (soc.trim()) form.set('soc', soc.trim());
    if (note.trim()) form.set('note', note.trim());
    if (ipctool.trim()) form.set('yaml', ipctool);
    if (bootLog.trim()) form.append('boot_log', new Blob([bootLog], { type: 'text/plain' }), 'boot_log.txt');
    for (const f of photos) form.append('photo', f, f.name);
    if (dump) {
      form.append('backup', dump, dump.name);
      form.set('consent', publicDump ? 'public' : 'private');
    }
    setState({ s: 'sending' });
    sendReport(form)
      .then((sent) => setState({ s: 'sent', sent }))
      .catch((err: Error) => setState({ s: 'error', error: t('club.send_failed', { error: err.message }) }));
  };

  if (state.s === 'sent') {
    const id = state.sent.id;
    return (
      <div class="grid gap-2 rounded-md bg-[#e3f5ec] px-3 py-2.5 text-sm text-[#146c3c]" role="status">
        <p class="m-0">{t(member ? 'club.sent_member' : 'club.sent_guest', { id })}</p>
        <p class="m-0">
          {member
            ? <a href={pathFor(locale, '/club')}>{t('club.my_link')}</a>
            : <a href={`${pathFor(locale, '/cameras/report')}?id=${id}`}>{t('club.receipt')}</a>}
        </p>
      </div>
    );
  }

  return (
    <form class="grid gap-3 rounded-lg border-[1.5px] border-dashed border-[#e8c58f] bg-white p-4 text-sm" onSubmit={submit}>
      <p class="m-0 text-body-secondary">{t('club.new_lede', { n: STARS.item, dump: STARS.dump })}</p>
      <div class="grid gap-3 sm:grid-cols-[1fr_1fr_10rem]">
        <label class="grid gap-1 text-[12.5px] text-body-secondary">{t('club.new_maker')}
          <input required value={maker} maxLength={80} placeholder={t('club.new_maker_hint')}
            onInput={(e) => setMaker((e.target as HTMLInputElement).value)} class={field} />
        </label>
        <label class="grid gap-1 text-[12.5px] text-body-secondary">{t('club.new_board')}
          <input required value={board} maxLength={80} placeholder={t('club.new_board_hint')}
            onInput={(e) => setBoard((e.target as HTMLInputElement).value)} class={field} />
        </label>
        <label class="grid gap-1 text-[12.5px] text-body-secondary">{t('club.new_soc')}
          <input value={soc} maxLength={40} placeholder="SSC335"
            onInput={(e) => setSoc((e.target as HTMLInputElement).value)} class={field} />
        </label>
      </div>
      <label class="grid gap-1 text-[12.5px] text-body-secondary">{t('club.new_photos')}
        <input type="file" multiple required accept="image/jpeg,image/png,image/webp"
          onChange={(e) => setPhotos([...((e.target as HTMLInputElement).files ?? [])])} class="text-sm text-body" />
      </label>
      <div class="grid gap-1">
        <label class="grid gap-1 text-[12.5px] text-body-secondary">{t('club.send_dump')}
          <input type="file" onChange={(e) => setDump((e.target as HTMLInputElement).files?.[0] ?? null)} class="text-sm text-body" />
        </label>
        <p class="m-0 text-[12px] text-body-secondary">{t('club.new_dump_hint', { n: STARS.dump })}</p>
        {dump && (
          <label class="flex items-start gap-2 text-[12.5px] text-body-secondary">
            <input type="checkbox" checked={publicDump} onChange={(e) => setPublicDump((e.target as HTMLInputElement).checked)} class="mt-0.5" />
            {t('club.send_public')}
          </label>
        )}
      </div>
      <details class="text-[12.5px] text-body-secondary">
        <summary class="cursor-pointer text-brand-blue">{t('club.new_more')}</summary>
        <div class="mt-2 grid gap-3">
          <label class="grid gap-1">{t('club.new_ipctool')}
            <textarea value={ipctool} onInput={(e) => setIpctool((e.target as HTMLTextAreaElement).value)} class={area} />
          </label>
          <label class="grid gap-1">{t('club.new_boot_log')}
            <textarea value={bootLog} onInput={(e) => setBootLog((e.target as HTMLTextAreaElement).value)} class={area} />
          </label>
        </div>
      </details>
      <input aria-label={t('club.send_note')} placeholder={t('club.send_note')} value={note}
        onInput={(e) => setNote((e.target as HTMLInputElement).value)} class={field} />
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <button type="submit" class="site-btn site-btn-primary site-btn-sm" disabled={state.s === 'sending'}>
          {state.s === 'sending' ? t('club.sending') : t('club.send_button')}
        </button>
        {member !== undefined && (
          <span class="text-[12.5px] text-body-secondary">
            {member
              ? t('club.send_signed_in', { name: member.name })
              : <>{t('club.send_guest')} <a href={pathFor(locale, '/club')}>{t('club.sign_in_link')}</a></>}
          </span>
        )}
      </div>
      {state.s === 'error' && <p class="m-0 text-[13px] text-[#a3262e]" role="alert">{state.error}</p>}
    </form>
  );
}
