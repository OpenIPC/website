/**
 * The board panel's "Have this board?" ask, answered on the page: paste a
 * boot log or a U-Boot console, drop photos or a flash dump, and it goes to
 * the owner-reports queue for the board (POST /api/v1/club/reports, channel
 * web, model = the board). It replaced a prefilled GitHub issue (#365, #366).
 *
 * Signing in is not required. A signed-in sender's report is theirs: it is
 * listed on /club with its state, its dump is private to them and the
 * maintainers, and it earns stars once accepted.
 */
import { useEffect, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { STARS, fetchMe, remembered, sendReport, type Member, type Sent } from '../../lib/club';
import IpctoolCode from './IpctoolCode';

export type SendKind = 'boot_log' | 'uboot_env' | 'photo' | 'backup';
const KINDS: SendKind[] = ['uboot_env', 'boot_log', 'photo', 'backup'];

export default function SendForm({ model, kind, locale, t, note }: {
  model: string; kind: SendKind; locale: Locale; t: BoardsT; note?: string;
}) {
  const [chosen, setChosen] = useState<SendKind>(kind);
  const [text, setText] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [extra, setExtra] = useState(note ?? '');
  const [publicDump, setPublicDump] = useState(false);
  // undefined while a browser that has signed in before asks who it is:
  // the line under the button says nothing until it knows.
  const [member, setMember] = useState<Member | null | undefined>(() => (remembered() ? undefined : null));
  const [state, setState] = useState<{ s: 'idle' | 'sending' } | { s: 'sent'; sent: Sent } | { s: 'error'; error: string }>({ s: 'idle' });

  useEffect(() => {
    if (remembered()) fetchMe().then((me) => setMember(me.member)).catch(() => setMember(null));
  }, []);

  const textual = chosen === 'boot_log' || chosen === 'uboot_env';
  const submit = (e: Event) => {
    e.preventDefault();
    if (!text.trim() && files.length === 0 && !extra.trim()) {
      setState({ s: 'error', error: t('club.send_empty') });
      return;
    }
    const form = new FormData();
    form.set('channel', 'web');
    form.set('model', model);
    if (extra.trim()) form.set('note', extra.trim());
    if (textual && text.trim()) form.append(chosen, new Blob([text], { type: 'text/plain' }), `${chosen}.txt`);
    for (const f of files) form.append(chosen, f, f.name);
    if (chosen === 'backup') form.set('consent', publicDump ? 'public' : 'private');
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
        <p class="m-0 flex flex-wrap gap-x-4">
          {member
            ? <a href={pathFor(locale, '/club')}>{t('club.my_link')}</a>
            : <a href={`${pathFor(locale, '/cameras/report')}?id=${id}`}>{t('club.receipt')}</a>}
          <button type="button" class="cursor-pointer p-0 text-brand-blue underline" onClick={() => { setText(''); setFiles([]); setState({ s: 'idle' }); }}>
            {t('club.send_another')}
          </button>
        </p>
        {member && (
          <div class="text-body">
            <IpctoolCode joins={id} locale={locale} t={t} label={t('club.code_add_ipctool')} />
          </div>
        )}
      </div>
    );
  }

  const reward = (k: SendKind) => t('club.reward', { n: k === 'backup' ? STARS.dump : STARS.item });
  return (
    <form class="grid gap-2.5 rounded-lg border-[1.5px] border-dashed border-[#e8c58f] bg-white p-3.5 text-sm" onSubmit={submit}>
      <div class="flex flex-wrap gap-1.5" role="group" aria-label={t('club.send_kinds_label')}>
        {KINDS.map((k) => (
          <button key={k} type="button" aria-pressed={chosen === k} onClick={() => { setChosen(k); setFiles([]); }}
            class={`cursor-pointer rounded-md border px-2.5 py-1 text-[13px] ${chosen === k ? 'border-brand-blue bg-[#eef0fc] font-semibold text-[#3d4dad]' : 'border-hairline bg-white text-body'}`}>
            {t(`club.send_kind_${k}`)} <span class="font-semibold text-[#9a5b00]">{reward(k)}</span>
          </button>
        ))}
      </div>
      {textual && (
        <textarea aria-label={t('club.send_paste')} placeholder={t('club.send_paste')} value={text}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          class="min-h-[96px] w-full resize-y rounded-md border border-hairline bg-surface-alt p-2 font-mono text-[12.5px] leading-snug" />
      )}
      <label class="grid gap-1 text-[12.5px] text-body-secondary">
        {chosen === 'photo' ? t('club.send_photos') : chosen === 'backup' ? t('club.send_dump') : t('club.send_file')}
        <input type="file" multiple={chosen === 'photo'} accept={chosen === 'photo' ? 'image/jpeg,image/png,image/webp' : chosen === 'backup' ? '' : '.txt,.log,text/plain'}
          onChange={(e) => setFiles([...((e.target as HTMLInputElement).files ?? [])])} class="text-sm text-body" />
      </label>
      {chosen === 'backup' && (
        <label class="flex items-start gap-2 text-[12.5px] text-body-secondary">
          <input type="checkbox" checked={publicDump} onChange={(e) => setPublicDump((e.target as HTMLInputElement).checked)} class="mt-0.5" />
          {t('club.send_public')}
        </label>
      )}
      <input aria-label={t('club.send_note')} placeholder={t('club.send_note')} value={extra}
        onInput={(e) => setExtra((e.target as HTMLInputElement).value)} class="rounded-md border border-hairline px-2.5 py-1.5" />
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
