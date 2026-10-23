/**
 * A member changing a report of theirs from /club
 * (POST /api/v1/club/reports/{id}/edit): the note, the camera they proposed,
 * files taken out, photos and text added, and a flash dump added to a report
 * that has none (#456). ipctool's output and a backup stay as sent. A report a maintainer has decided goes back to them: the form says
 * so before it is saved. On a report a maintainer asked about, saving is the
 * answer, and the form says that.
 */
import { useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { editReport, type MemberReport } from '../../lib/club';
import PhotoChecks from './PhotoChecks';

type AddKind = 'photo' | 'boot_log' | 'uboot_env' | 'backup';
const ADD_KINDS: AddKind[] = ['photo', 'boot_log', 'uboot_env', 'backup'];
const field = 'rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body';

export default function EditReport({ report, t, onSaved, onCancel }: {
  report: MemberReport; t: BoardsT; onSaved: () => void; onCancel: () => void;
}) {
  const [note, setNote] = useState(report.note ?? '');
  const [maker, setMaker] = useState(report.proposal?.maker ?? '');
  const [board, setBoard] = useState(report.proposal?.board ?? '');
  const [soc, setSoc] = useState(report.proposal?.soc ?? '');
  const [remove, setRemove] = useState<Set<number>>(new Set());
  const [kind, setKind] = useState<AddKind>('photo');
  const [add, setAdd] = useState<File[]>([]);
  const [publicDump, setPublicDump] = useState(false);
  const [own, setOwn] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const decided = report.status === 'published' || report.status === 'rejected';
  // One dump a report: the one it has stays as sent, so none is offered.
  const kinds = report.files.some((f) => f.kind === 'backup') ? ADD_KINDS.filter((k) => k !== 'backup') : ADD_KINDS;

  const toggle = (pos: number) => {
    const next = new Set(remove);
    if (next.has(pos)) next.delete(pos); else next.add(pos);
    setRemove(next);
  };
  const save = (e: Event) => {
    e.preventDefault();
    const photos = kind === 'photo' && add.length > 0;
    if (photos && !own) {
      setError(t('club.photo_own_required'));
      return;
    }
    const form = new FormData();
    if (note.trim() !== (report.note ?? '')) form.set('note', note.trim());
    if (report.proposal && (maker.trim() !== report.proposal.maker || board.trim() !== report.proposal.board || soc.trim() !== (report.proposal.soc ?? ''))) {
      form.set('maker', maker.trim());
      form.set('board', board.trim());
      form.set('soc', soc.trim());
    }
    for (const pos of remove) form.append('remove', String(pos));
    for (const f of add) form.append(kind, f, f.name);
    if (kind === 'backup' && add.length > 0) form.set('consent', publicDump ? 'public' : 'private');
    if (photos) form.set('own_photos', 'yes');
    setBusy(true);
    setError(null);
    editReport(report.id, form)
      .then(onSaved)
      .catch((err: Error) => setError(t('club.send_failed', { error: err.message })))
      .finally(() => setBusy(false));
  };

  return (
    <form class="grid gap-2.5 rounded-md border border-hairline bg-surface-alt p-3 text-[13px]" onSubmit={save}>
      {decided && <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-[#8a4b00]" role="note">{t('club.edit_rereview')}</p>}
      {report.status === 'asked' && <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-[#8a4b00]" role="note">{t('club.edit_answer')}</p>}
      {report.proposal && (
        <div class="grid gap-2 sm:grid-cols-[1fr_1fr_8rem]">
          <label class="grid gap-1 text-body-secondary">{t('club.new_maker')}
            <input value={maker} maxLength={80} onInput={(e) => setMaker((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="grid gap-1 text-body-secondary">{t('club.new_board')}
            <input value={board} maxLength={80} onInput={(e) => setBoard((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="grid gap-1 text-body-secondary">{t('club.new_soc')}
            <input value={soc} maxLength={40} onInput={(e) => setSoc((e.target as HTMLInputElement).value)} class={field} />
          </label>
        </div>
      )}
      <label class="grid gap-1 text-body-secondary">{t('club.your_note')}
        <textarea value={note} maxLength={4000} onInput={(e) => setNote((e.target as HTMLTextAreaElement).value)}
          class={`${field} min-h-[56px] resize-y`} />
      </label>
      {report.files.some((f) => f.kind !== 'backup') && (
        <fieldset class="m-0 grid gap-1 border-0 p-0">
          <legend class="mb-1 p-0 text-body-secondary">{t('club.edit_remove')}</legend>
          {report.files.filter((f) => f.kind !== 'backup').map((f) => (
            <label key={f.position} class="flex items-center gap-2">
              <input type="checkbox" checked={remove.has(f.position)} onChange={() => toggle(f.position)} />
              <span class={remove.has(f.position) ? 'line-through text-body-secondary' : ''}>{t(`club.kind_${f.kind}`)} · {f.name}</span>
            </label>
          ))}
        </fieldset>
      )}
      <div class="grid gap-1">
        <span class="text-body-secondary">{t('club.edit_add')}</span>
        <div class="flex flex-wrap items-center gap-2">
          <select value={kind} onChange={(e) => { setKind((e.target as HTMLSelectElement).value as AddKind); setAdd([]); }} class={field}>
            {kinds.map((k) => <option key={k} value={k}>{t(`club.kind_${k}`)}</option>)}
          </select>
          {/* keyed on the kind: switching it clears what was picked, input and all */}
          <input key={kind} type="file" multiple={kind !== 'backup'} aria-label={t('club.edit_add')}
            accept={kind === 'photo' ? 'image/jpeg,image/png,image/webp' : kind === 'backup' ? '' : '.txt,.log,text/plain'}
            onChange={(e) => setAdd([...((e.target as HTMLInputElement).files ?? [])])} class="text-sm text-body" />
        </div>
        {kind === 'photo' && <PhotoChecks files={add} onFiles={setAdd} own={own} onOwn={setOwn} t={t} />}
        {(kind === 'boot_log' || kind === 'uboot_env') && <p class="m-0 text-[12px] text-body-secondary">{t(`club.send_hint_${kind}`)}</p>}
        {kind === 'backup' && (
          <>
            <p class="m-0 text-[12px] text-body-secondary">{t('club.send_dump')}</p>
            <label class="flex items-start gap-2 text-[12.5px] text-body-secondary">
              <input type="checkbox" checked={publicDump} onChange={(e) => setPublicDump((e.target as HTMLInputElement).checked)} class="mt-0.5" />
              {t('club.send_public')}
            </label>
          </>
        )}
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button type="submit" class="site-btn site-btn-primary site-btn-sm" disabled={busy}>{t(decided ? 'club.edit_save_rereview' : 'club.edit_save')}</button>
        <button type="button" class="site-btn site-btn-outline-secondary site-btn-sm" onClick={onCancel}>{t('club.edit_cancel')}</button>
        {error && <span class="text-[#a3262e]" role="alert">{error}</span>}
      </div>
    </form>
  );
}
