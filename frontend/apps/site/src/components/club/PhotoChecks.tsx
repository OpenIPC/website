/**
 * The photos a send form is about to upload, checked first. A photo with a
 * QR code or a barcode in it -- the label a serial number is printed on --
 * is flagged, with a button that blurs the codes and the label around them;
 * any photo can have regions blurred by hand. The blur happens here, in
 * the browser: the original never leaves it. Below, the confirmation every
 * form with photos asks for: they are of the sender's own camera, taken by
 * them (a photo of a screen showing someone else's board once nearly put
 * the wrong PCB into the catalogue). The form sends it as own_photos=yes.
 *
 * Until every photo has been looked at and every blur asked for is done,
 * onBusy says so and the form does not send: an original the sender asked
 * to blur is never uploaded, nor a photo whose code warning has not come
 * yet. A blur that failed keeps it so until the photo is blurred again or
 * the photos are chosen anew.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { blurFile, findCodes, type Box } from '../../lib/photo-check';
import RegionPicker from './RegionPicker';

type Check = { codes: Box[] | null; blurred?: boolean; blurring?: boolean; failed?: boolean };

/** The parent's file list, changed from what it is now -- never from a copy taken earlier. */
export type FilesUpdate = (update: (files: File[]) => File[]) => void;

/** Whether the photos are still being looked at or blurred. */
export function photosBusy(files: File[], checks: Map<File, Check>): boolean {
  return files.some((f) => {
    const c = checks.get(f);
    return !c || !!c.blurring || !!c.failed;
  });
}

export default function PhotoChecks({ files, onFiles, own, onOwn, onBusy, t }: {
  files: File[]; onFiles: FilesUpdate; own: boolean; onOwn: (own: boolean) => void; onBusy: (busy: boolean) => void; t: BoardsT;
}) {
  const [checks, setChecks] = useState<Map<File, Check>>(new Map());
  const [picking, setPicking] = useState<{ file: File; url: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const started = useRef(new WeakSet<File>());
  const mounted = useRef(true);
  const inFlight = useRef(new WeakSet<File>());

  const set = (f: File, c: Check) => setChecks((m) => new Map(m).set(f, c));

  useEffect(() => () => { mounted.current = false; onBusy(false); }, []);
  useEffect(() => {
    for (const f of files) {
      if (started.current.has(f)) continue;
      started.current.add(f);
      void findCodes(f).then((codes) => {
        if (mounted.current) setChecks((m) => (m.has(f) ? m : new Map(m).set(f, { codes })));
      });
    }
  }, [files]);
  const busy = photosBusy(files, checks);
  useEffect(() => onBusy(busy), [busy]);
  useEffect(() => () => { if (picking) URL.revokeObjectURL(picking.url); }, [picking]);

  const replace = (old: File, boxes: Box[]) => {
    // One blur of a photo at a time, from the photo as it is now.
    if (inFlight.current.has(old)) return;
    inFlight.current.add(old);
    const was = checks.get(old) ?? { codes: null };
    setError(null);
    set(old, { ...was, blurring: true, failed: false });
    blurFile(old, boxes)
      .then((done) => {
        if (!mounted.current) return;
        started.current.add(done);
        setChecks((m) => { const n = new Map(m); n.delete(old); return n.set(done, { codes: [], blurred: true }); });
        onFiles((now) => now.map((f) => (f === old ? done : f)));
        setPicking((p) => (p?.file === old ? null : p));
      })
      .catch((e: Error) => {
        if (!mounted.current) return;
        set(old, { ...was, blurring: false, failed: true });
        setError(t('club.photo_blur_failed', { error: e.message }));
      })
      .finally(() => inFlight.current.delete(old));
  };
  const pick = (f: File) => {
    if (typeof URL.createObjectURL !== 'function') return;
    setPicking({ file: f, url: URL.createObjectURL(f) });
  };

  if (files.length === 0) return null;
  return (
    <div class="grid gap-1.5 text-[12.5px]">
      <ul class="m-0 grid list-none gap-1.5 p-0">
        {files.map((f) => {
          const c = checks.get(f);
          const codes = c?.codes?.length ?? 0;
          return (
            <li key={f.name + f.size} class="grid gap-1">
              <span class="flex flex-wrap items-center gap-x-2">
                <span class="font-mono">{f.name}</span>
                {c?.blurred && <span class="rounded-full bg-[#e3f5ec] px-2 text-[11.5px] font-semibold text-[#146c3c]">{t('club.photo_blurred')}</span>}
                <button type="button" class="cursor-pointer p-0 text-brand-blue underline disabled:opacity-50" disabled={!!c?.blurring}
                  onClick={() => pick(f)}>{t('club.photo_blur_region')}</button>
              </span>
              {codes > 0 && !c?.blurred && (
                <p class="m-0 flex flex-wrap items-center gap-2 rounded-md bg-[#fff4e2] px-2.5 py-1.5 text-[#8a4b00]" role="alert">
                  {t('club.photo_code_found', { n: codes })}
                  <button type="button" class="site-btn site-btn-outline-secondary site-btn-sm" disabled={!!c?.blurring}
                    onClick={() => replace(f, c!.codes!)}>
                    {t('club.photo_blur_codes')}
                  </button>
                </p>
              )}
            </li>
          );
        })}
      </ul>
      {picking && (
        // Keyed by the photo: another photo's picker starts with no regions.
        <RegionPicker key={picking.url} src={picking.url} alt={picking.file.name} t={t}
          onSave={(boxes) => replace(picking.file, boxes)} onCancel={() => setPicking(null)} />
      )}
      {busy && <p class="m-0 text-body-secondary" role="status">{t('club.photo_processing')}</p>}
      {error && <p class="m-0 text-[#a3262e]" role="alert">{error}</p>}
      <label class="flex items-start gap-2 text-body-secondary">
        <input type="checkbox" checked={own} required onChange={(e) => onOwn((e.target as HTMLInputElement).checked)} class="mt-0.5" />
        {t('club.photo_own')}
      </label>
    </div>
  );
}
