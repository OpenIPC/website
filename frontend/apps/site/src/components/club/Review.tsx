/**
 * /club/review: the maintainers' review queue. Each report shows who sent it,
 * the board they named or ipctool's guess, its files (private dumps
 * included, for the reviewer), and what publishing it would earn the sender.
 * Publishing links it to its boards, lists its text there, awards the stars
 * and tells the sender; rejecting takes back anything it had earned. A
 * report about a camera the catalogue does not have shows the board
 * publishing would add, for the reviewer to correct; publishing adds it.
 * Each photo says whether its sender confirmed it is of their own camera,
 * and can have regions blurred in the copy that is published (the service
 * blurs it from the original) or be kept private as not the sender's unit.
 *
 * Reports of one camera (an identifier's keyed hash in common) stand
 * together, each naming the others and the boards they are published on,
 * which the board ids then start from. One that adds nothing to another --
 * the same files, the same ipctool output, no note of its own -- says so,
 * and is rejected as its duplicate with one click.
 *
 * A report that cannot be placed as it is -- no board marking, a marking
 * that disagrees with the photo, a label that should not be published --
 * is asked about instead: the question goes to a member sender by the bot
 * or by mail, and onto the receipt of one who sent it signed out. It waits
 * under "Asked" until the sender's edit puts it back under "Waiting", the
 * question beside it.
 *
 * The same decisions remain available as `openipc reports publish|reject|ask`.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { boardPath } from '../../lib/boards/shell';
import { ClubError, ask, decide, fetchMe, fetchQueue, reviewPhoto, type LinkBasis, type Member, type MemberFile, type NewBoard, type Queued, type Sibling } from '../../lib/club';
import { size, waiting } from '../../lib/reports';
import { Forbidden, STATUS_TONE } from './parts';
import RegionPicker from './RegionPicker';
import type { Box } from '../../lib/photo-check';

type Load = { state: 'loading' } | { state: 'ok'; list: Queued[] } | { state: 'forbidden' } | { state: 'signed_out' } | { state: 'error' };
const TABS = ['pending', 'asked', 'published', 'rejected'] as const;

export default function Review({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [tab, setTab] = useState<(typeof TABS)[number]>('pending');
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [me, setMe] = useState<Member | null>(null);

  const reload = () => {
    setLoad({ state: 'loading' });
    fetchQueue(tab)
      .then((r) => setLoad({ state: 'ok', list: r.reports }))
      .catch((e) => {
        if (e instanceof ClubError && e.status === 401) setLoad({ state: 'signed_out' });
        else if (e instanceof ClubError && e.status === 403) setLoad({ state: 'forbidden' });
        else setLoad({ state: 'error' });
      });
  };
  useEffect(() => { void fetchMe().then((r) => setMe(r.member)).catch(() => {}); }, []);
  useEffect(reload, [tab]);

  if (load.state === 'signed_out' || load.state === 'forbidden') return <Forbidden why={t('club.review_forbidden')} member={me} locale={locale} t={t} />;
  const listed = new Set(load.state === 'ok' ? load.list.map((q) => q.id) : []);
  return (
    <div class="mt-8 grid gap-4">
      <div class="flex flex-wrap gap-2" role="tablist">
        {TABS.map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}
            class={`cursor-pointer rounded-md border px-3 py-1 text-sm ${tab === k ? 'border-brand-blue bg-[#eef0fc] font-semibold text-[#3d4dad]' : 'border-hairline bg-white'}`}>
            {t(`club.tab_${k}`)}
          </button>
        ))}
      </div>
      {load.state === 'loading' && <div class="h-40 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />}
      {load.state === 'error' && <p class="m-0 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {load.state === 'ok' && load.list.length === 0 && <p class="m-0 text-body-secondary">{t('club.review_empty')}</p>}
      {load.state === 'ok' && byCamera(load.list).map((g) => {
        const items = g.map((q) => <Item key={q.id} q={q} listed={listed} locale={locale} t={t} onDone={reload} />);
        return g.length === 1 ? items : (
          <section key={g[0].id} class="grid gap-3 rounded-xl border-s-4 border-brand-blue ps-3" aria-label={t('club.kin_group', { n: g.length })}>
            <p class="m-0 text-[13px] font-semibold text-[#3d4dad]">{t('club.kin_group', { n: g.length })}</p>
            {items}
          </section>
        );
      })}
    </div>
  );
}

/**
 * The queue in groups of one camera, each where its newest report stood:
 * reports linked by same_camera, directly or through another in the list.
 */
function byCamera(list: Queued[]): Queued[][] {
  const root = new Map(list.map((q) => [q.id, q.id]));
  const find = (id: string): string => {
    const up = root.get(id)!;
    return up === id ? id : find(up);
  };
  for (const q of list) {
    for (const s of q.same_camera ?? []) {
      if (root.has(s.id)) root.set(find(s.id), find(q.id));
    }
  }
  const groups = new Map<string, Queued[]>();
  for (const q of list) {
    const k = find(q.id);
    groups.set(k, [...(groups.get(k) ?? []), q]);
  }
  return [...groups.values()];
}

/** The boards the camera's published reports are on: where this one belongs too. */
function siblingBoards(q: Queued): string[] {
  const ids = [q.duplicate_of, ...(q.same_camera ?? [])]
    .filter((s): s is Sibling => !!s && s.status === 'published')
    .flatMap((s) => (s.boards ?? []).map((b) => b.id));
  return [...new Set(ids)];
}

const NEW_FIELDS: (keyof NewBoard)[] = ['maker_name', 'maker_id', 'model', 'model_id', 'soc'];
const CERTAINTIES: LinkBasis['certainty'][] = ['confirmed', 'probable', 'guess'];

function Item({ q, listed, locale, t, onDone }: { q: Queued; listed: Set<string>; locale: Locale; t: BoardsT; onDone: () => void }) {
  const sug = q.new_board;
  const known = siblingBoards(q);
  const [models, setModels] = useState(q.models.length ? q.models.join(', ') : known.length ? known.join(', ') : sug?.existing ?? '');
  const [create, setCreate] = useState(!!sug && !sug.existing && !known.length && q.status !== 'published');
  const [board, setBoard] = useState<NewBoard | null>(sug
    ? { maker_id: sug.maker_id, maker_name: sug.maker_name, model_id: sug.model_id, model: sug.model, soc: sug.soc ?? '' }
    : null);
  const [note, setNote] = useState('');
  const [certainty, setCertainty] = useState<LinkBasis['certainty']>('confirmed');
  const [evidence, setEvidence] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const received = new Date(q.received_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const photos = q.file_list.filter((f) => f.kind === 'photo');

  const act = (decision: 'publish' | 'reject', why = note) => {
    // A link that is not confirmed says why; the service refuses it otherwise.
    if (decision === 'publish' && certainty !== 'confirmed' && !evidence.trim()) {
      setError(t('club.review_evidence_needed'));
      return;
    }
    setBusy(true);
    setError(null);
    const ids = models.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
    decide(q.id, decision, ids, why, decision === 'publish' && create && board ? board : undefined, q.revision,
      decision === 'publish' ? { certainty, evidence: evidence.trim() } : undefined)
      .then((r) => {
        setResult(r.board ? t('club.review_done_board', { board: r.board, points: r.points }) : t(`club.review_done_${decision}`, { points: r.points }));
        setTimeout(onDone, 1200);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  };

  return (
    <article class="grid gap-3 rounded-xl border border-hairline p-4" aria-labelledby={`q-${q.id}`}>
      <header class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id={`q-${q.id}`} class="m-0 text-base font-semibold">
          {q.board ? `${q.board.manufacturer} ${q.board.model}` : q.proposal ? `${q.proposal.maker} ${q.proposal.board}` : (q.chip || q.id)}
          <span class="ms-2 font-mono text-[12px] font-normal text-body-secondary">{q.id} · {q.channel}</span>
        </h2>
        <span class="flex items-center gap-2 text-[13px] text-body-secondary">
          {received}
          <span class={`rounded-full px-2.5 py-0.5 text-[12.5px] font-semibold ${STATUS_TONE[q.status]}`}>{t(`club.status_${q.status}`)}</span>
        </span>
      </header>
      <dl class="m-0 grid grid-cols-1 gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_1fr]">
        <dt class="text-body-secondary">{t('club.col_submission')}</dt>
        <dd class="m-0">{q.member ? t('club.review_from', { who: q.member }) : t('club.review_anon')}</dd>
        {q.board && <><dt class="text-body-secondary">{t('club.review_board')}</dt>
          <dd class="m-0"><a href={pathFor(locale, boardPath(q.board.id))} class="font-mono">{q.board.id}</a></dd></>}
        {q.edited_at && <><dt class="text-body-secondary">{t('club.review_edited_label')}</dt>
          <dd class="m-0">{new Date(q.edited_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</dd></>}
        {q.joins && <><dt class="text-body-secondary">{t('club.review_joins_label')}</dt>
          <dd class="m-0">{t('club.goes_with', { id: q.joins })}</dd></>}
        {q.answers && <><dt class="text-body-secondary">{t('club.review_answers_label')}</dt>
          <dd class="m-0"><a href={`${pathFor(locale, '/cameras/report')}?id=${q.answers}`} class="font-mono">{q.answers}</a></dd></>}
        {!!q.answered_by?.length && <><dt class="text-body-secondary">{t('club.review_answered_by_label')}</dt>
          <dd class="m-0">{q.answered_by.map((id, i) => <span key={id}>{i > 0 && ', '}<a href={`#q-${id}`} class="font-mono">{id}</a></span>)}</dd></>}
        {q.proposal && <><dt class="text-body-secondary">{t('club.review_proposal')}</dt>
          <dd class="m-0">{[q.proposal.maker, q.proposal.board, q.proposal.soc].filter(Boolean).join(' · ')}</dd></>}
        {q.guess && <><dt class="text-body-secondary">ipctool</dt><dd class="m-0">{t('club.review_guess', { board: `${q.guess.manufacturer} ${q.guess.model}` })} <span class="font-mono text-[12px]">{q.guess.model_id}</span></dd></>}
        {(q.chip || q.sensor) && <><dt class="text-body-secondary">SoC</dt><dd class="m-0">{[q.chip, q.sensor].filter(Boolean).join(' · ')}</dd></>}
        {q.note && <><dt class="text-body-secondary">{t('club.kind_note')}</dt><dd class="m-0 whitespace-pre-wrap">{q.note}</dd></>}
        {q.question && waiting(q.status) && <><dt class="text-body-secondary">{t(q.status === 'asked' ? 'club.review_asked' : 'club.review_answered')}</dt>
          <dd class="m-0 whitespace-pre-wrap">{q.question}</dd></>}
        {photos.length > 0 && <><dt class="text-body-secondary">{t('club.review_own_label')}</dt>
          <dd class={`m-0 ${photos.every((f) => f.own) ? '' : 'font-semibold text-[#9a5b00]'}`}>
            {t(photos.every((f) => f.own) ? 'club.review_own_yes' : photos.some((f) => f.own) ? 'club.review_own_some' : 'club.review_own_no')}
          </dd></>}
      </dl>
      {(q.duplicate_of || q.same_camera?.length || q.same_sender?.length) ? (
        <ul class="m-0 grid list-none gap-1 p-0 text-[13px]" aria-label={t('club.kin_label')}>
          {q.duplicate_of && <Kin label={t('club.kin_duplicate')} s={q.duplicate_of} strong listed={listed} locale={locale} t={t} />}
          {q.same_camera?.map((s) => <Kin key={s.id} label={t('club.kin_camera')} s={s} listed={listed} locale={locale} t={t} />)}
          {q.same_sender?.map((s) => <Kin key={s.id} label={t('club.kin_sender')} s={s} weak listed={listed} locale={locale} t={t} />)}
        </ul>
      ) : null}
      <ul class="m-0 grid list-none gap-1 p-0 text-sm">
        {q.file_list.map((f) => (
          <li key={f.position} class="flex flex-wrap items-center gap-x-2">
            <a href={f.url} download={f.name}>{t(`club.kind_${f.kind}`)} · {f.name}</a>
            <span class="text-[12.5px] text-body-secondary tabular-nums">{size(f.bytes)}</span>
            {f.private && f.kind !== 'photo' && <span class="rounded-full bg-[#eef0fc] px-2 text-[11.5px] font-semibold text-[#3d4dad]">{t('club.private')}</span>}
            <span class="text-[12.5px] font-semibold text-[#9a5b00]">{f.points > 0 ? `+${f.points} ★` : f.kind === 'backup' ? t('club.duplicate') : ''}</span>
            {f.kind === 'photo' && <PhotoTools id={q.id} f={f} t={t} onDone={onDone} />}
          </li>
        ))}
      </ul>
      {q.yaml && (
        <details class="text-sm">
          <summary class="cursor-pointer text-brand-blue">{t('club.show_yaml')}</summary>
          <pre class="mt-2 max-h-72 overflow-auto rounded-md bg-surface-alt p-3 font-mono text-[12.5px]">{q.yaml}</pre>
        </details>
      )}
      <div class="grid gap-2 border-t border-hairline pt-3">
            {sug?.existing && q.status !== 'published' && (
              <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-sm text-[#9a5b00]">{t('club.review_existing', { board: sug.existing })}</p>
            )}
            {!sug?.existing && !!sug?.likely?.length && q.status !== 'published' && (
              <p class="m-0 rounded-md bg-surface-alt px-3 py-2 text-sm text-body-secondary">{t('club.review_likely', { boards: sug.likely.join(', ') })}</p>
            )}
            {board && q.status !== 'published' && (
              <fieldset class="m-0 grid gap-2 rounded-md border border-hairline p-3">
                <legend class="px-1 text-[13px]">
                  <label class="flex items-center gap-2">
                    <input type="checkbox" checked={create} onChange={(e) => setCreate((e.target as HTMLInputElement).checked)} />
                    {t('club.review_new_board')}
                    {sug && <span class="text-body-secondary">· {t(sug.maker_known ? 'club.review_maker_known' : 'club.review_maker_new')}</span>}
                  </label>
                </legend>
                {create && (
                  <div class="grid gap-2 sm:grid-cols-2">
                    {NEW_FIELDS.map((k) => (
                      <label key={k} class="grid gap-1 text-[13px] text-body-secondary">{t(`club.review_new_${k}`)}
                        <input value={board[k] ?? ''} onInput={(e) => setBoard({ ...board, [k]: (e.target as HTMLInputElement).value })}
                          class={`rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body ${k.endsWith('_id') ? 'font-mono' : ''}`} />
                      </label>
                    ))}
                  </div>
                )}
              </fieldset>
            )}
            <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_models')}
              <input value={models} onInput={(e) => setModels((e.target as HTMLInputElement).value)} placeholder={q.board?.id ?? ''}
                class="rounded-md border border-hairline px-2.5 py-1.5 font-mono text-sm text-body" />
            </label>
            {q.status !== 'published' && (
              <div class="grid gap-2 sm:grid-cols-[max-content_1fr]">
                <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_certainty')}
                  <select value={certainty} onChange={(e) => setCertainty((e.target as HTMLSelectElement).value as LinkBasis['certainty'])}
                    class="rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body">
                    {CERTAINTIES.map((c) => <option key={c} value={c}>{t(`club.review_certainty_${c}`)}</option>)}
                  </select>
                </label>
                <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_evidence')}
                  <input value={evidence} onInput={(e) => setEvidence((e.target as HTMLInputElement).value)} maxLength={200}
                    aria-required={certainty !== 'confirmed'} placeholder={t('club.review_evidence_hint')}
                    class="rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body" />
                </label>
                {q.models.length > 0 && <p class="m-0 text-[12.5px] text-body-secondary sm:col-span-2">{t('club.review_certainty_kept')}</p>}
              </div>
            )}
            <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.review_note')}
              <input value={note} onInput={(e) => setNote((e.target as HTMLInputElement).value)}
                class="rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body" />
            </label>
            <div class="flex flex-wrap items-center gap-2">
              {q.status !== 'published' && (
                <button type="button" disabled={busy} class="site-btn site-btn-primary" onClick={() => act('publish')}>
                  {t('club.publish')}{q.potential > 0 && ` · +${q.potential} ★`}
                </button>
              )}
              {q.status !== 'rejected' && <button type="button" disabled={busy} class="site-btn site-btn-outline-secondary" onClick={() => act('reject')}>{t('club.reject')}</button>}
              {q.duplicate_of && q.status !== 'rejected' && (
                <button type="button" disabled={busy} class="site-btn site-btn-outline-secondary" onClick={() => {
                  const why = t('club.duplicate_note', { id: q.duplicate_of!.id });
                  setNote(why);
                  act('reject', why);
                }}>{t('club.reject_duplicate', { id: q.duplicate_of.id })}</button>
              )}
              {waiting(q.status) && !asking && (
                <button type="button" disabled={busy} class="site-btn site-btn-outline-secondary" onClick={() => setAsking(true)}>{t('club.ask')}</button>
              )}
              {result && <span class="text-sm text-[#146c3c]" role="status">{result}</span>}
              {error && <span class="text-sm text-[#a3262e]" role="alert">{error}</span>}
            </div>
            {asking && <Ask q={q} t={t} onAsked={(r) => {
              setAsking(false);
              // Only what was sent is claimed: a member not reached reads it on /club.
              setResult(t(`club.review_done_ask_${r.told || (!q.member ? 'guest' : r.quiet ? 'member' : 'undelivered')}`));
              setTimeout(onDone, 1200);
            }} onCancel={() => setAsking(false)} />}
      </div>
    </article>
  );
}

/** One related report: what it is to this one, its id (on this page, or its receipt), its state and boards. */
function Kin({ label, s, strong, weak, listed, locale, t }: {
  label: string; s: Sibling; strong?: boolean; weak?: boolean; listed: Set<string>; locale: Locale; t: BoardsT;
}) {
  const href = listed.has(s.id) ? `#q-${s.id}` : `${pathFor(locale, '/cameras/report')}?id=${s.id}`;
  return (
    <li class={`flex flex-wrap items-center gap-x-1.5 ${strong ? 'font-semibold text-[#9a5b00]' : weak ? 'text-body-secondary' : ''}`}>
      {label} <a href={href} class="font-mono">{s.id}</a>
      <span class={`rounded-full px-2 text-[11.5px] font-semibold ${STATUS_TONE[s.status]}`}>{t(`club.status_${s.status}`)}</span>
      {s.status === 'published' && !!s.boards?.length && (
        <span class="font-normal">
          {t('club.kin_on')}{' '}
          {s.boards.map((b, i) => <span key={b.id}>{i > 0 && ', '}<a href={pathFor(locale, boardPath(b.id))} class="font-mono">{b.id}</a></span>)}
        </span>
      )}
    </li>
  );
}

/**
 * The question to the sender, with the ones the queue keeps needing a click
 * away: the board's marking, a marking that disagrees with what was typed,
 * a serial-number label in a photo.
 */
function Ask({ q, t, onAsked, onCancel }: {
  q: Queued; t: BoardsT; onAsked: (r: { told: string; quiet: boolean }) => void; onCancel: () => void;
}) {
  const [question, setQuestion] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const typed = q.proposal?.board ?? q.board?.model ?? 'Y';
  const templates = [
    t('club.ask_tpl_marking'),
    t('club.ask_tpl_mismatch', { board: typed }),
    t('club.ask_tpl_serial'),
  ];
  const send = (e: Event) => {
    e.preventDefault();
    if (!question.trim()) {
      setError(t('club.ask_empty'));
      return;
    }
    setBusy(true);
    setError(null);
    ask(q.id, question.trim(), q.revision)
      .then(onAsked)
      .catch((err: Error) => setError(err.message))
      .finally(() => setBusy(false));
  };
  return (
    <form class="grid gap-2 rounded-md border border-[#e8c58f] bg-[#fffaf0] p-3" onSubmit={send}>
      <div class="flex flex-wrap items-center gap-1.5 text-[13px]" role="group" aria-label={t('club.ask_templates')}>
        <span class="text-body-secondary">{t('club.ask_templates')}:</span>
        {templates.map((text) => (
          <button key={text} type="button" class="cursor-pointer rounded-md border border-hairline bg-white px-2 py-0.5 text-left text-[12.5px]"
            onClick={() => setQuestion(question.trim() ? `${question.trim()}\n${text}` : text)}>
            {text}
          </button>
        ))}
      </div>
      <label class="grid gap-1 text-[13px] text-body-secondary">{t('club.ask_label')}
        <textarea value={question} maxLength={2000} onInput={(e) => setQuestion((e.target as HTMLTextAreaElement).value)}
          class="min-h-[72px] w-full resize-y rounded-md border border-hairline px-2.5 py-1.5 text-sm text-body" />
      </label>
      <div class="flex flex-wrap items-center gap-2">
        <button type="submit" disabled={busy} class="site-btn site-btn-primary site-btn-sm">{t('club.ask_send')}</button>
        <button type="button" disabled={busy} class="site-btn site-btn-outline-secondary site-btn-sm" onClick={onCancel}>{t('club.ask_cancel')}</button>
        {error && <span class="text-sm text-[#a3262e]" role="alert">{error}</span>}
      </div>
    </form>
  );
}

const PUBLIC_TONE = { blurred: 'bg-[#e3f5ec] text-[#146c3c]', private: 'bg-[#fbe9ea] text-[#a3262e]' } as const;

/**
 * One photo's tools on the review card: whether its sender confirmed it is
 * theirs, what will be published, and the reviewer's three answers -- blur
 * regions of the published copy, keep it private (not the sender's unit),
 * or publish it as sent. The blur is the service's, from the original.
 */
function PhotoTools({ id, f, t, onDone }: { id: string; f: MemberFile; t: BoardsT; onDone: () => void }) {
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const act = (action: 'blur' | 'private' | 'as_sent', regions?: Box[]) => {
    setBusy(true);
    setError(null);
    reviewPhoto(id, f.position, action, regions)
      .then(() => { setPicking(false); onDone(); })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  };
  const link = 'cursor-pointer p-0 text-[12.5px] text-brand-blue underline disabled:opacity-50';
  return (
    <>
      <span class={`text-[12px] ${f.own ? 'text-[#146c3c]' : 'font-semibold text-[#9a5b00]'}`}>
        {t(f.own ? 'club.photo_own_yes' : 'club.photo_own_no')}
      </span>
      {f.public && f.public !== 'as_sent' && (
        <span class={`rounded-full px-2 text-[11.5px] font-semibold ${PUBLIC_TONE[f.public]}`}>{t(`club.photo_public_${f.public}`)}</span>
      )}
      {f.public_url && <a href={f.public_url} class="text-[12.5px]" target="_blank" rel="noopener">{t('club.photo_public_copy')}</a>}
      <span class="flex flex-wrap gap-x-3">
        <button type="button" class={link} disabled={busy} onClick={() => setPicking(!picking)}>{t('club.photo_blur_region')}</button>
        {f.public !== 'private' && (
          <button type="button" class={link} disabled={busy} onClick={() => act('private')}>{t('club.photo_not_unit')}</button>
        )}
        {f.public && f.public !== 'as_sent' && (
          <button type="button" class={link} disabled={busy} onClick={() => act('as_sent')}>{t('club.photo_as_sent')}</button>
        )}
      </span>
      {error && <span class="text-[12.5px] text-[#a3262e]" role="alert">{error}</span>}
      {picking && (
        <div class="basis-full">
          <RegionPicker src={f.url} alt={f.name} initial={f.regions} busy={busy} t={t}
            onSave={(boxes) => act('blur', boxes)} onCancel={() => setPicking(false)} />
        </div>
      )}
    </>
  );
}
