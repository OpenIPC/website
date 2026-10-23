/**
 * The photos a send form is about to upload, checked first. A photo with a
 * QR code or a barcode in it -- the label a serial number is printed on --
 * is flagged, with a button that blurs the codes and the label around them;
 * any photo can have regions blurred by hand. The blur happens here, in
 * the browser: the original never leaves it. Below, the confirmation every
 * form with photos asks for: they are of the sender's own camera, taken by
 * them (a photo of a screen showing someone else's board once nearly put
 * the wrong PCB into the catalogue). The form sends it as own_photos=yes.
 */
import { useEffect, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { blurFile, findCodes, type Box } from '../../lib/photo-check';
import RegionPicker from './RegionPicker';

type Check = { codes: Box[] | null; blurred: boolean };

export default function PhotoChecks({ files, onFiles, own, onOwn, t }: {
  files: File[]; onFiles: (files: File[]) => void; own: boolean; onOwn: (own: boolean) => void; t: BoardsT;
}) {
  const [checks, setChecks] = useState<Map<File, Check>>(new Map());
  const [picking, setPicking] = useState<{ file: File; url: string } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    for (const f of files) {
      if (checks.has(f)) continue;
      void findCodes(f).then((codes) => {
        if (live) setChecks((m) => new Map(m).set(f, { codes, blurred: m.get(f)?.blurred ?? false }));
      });
    }
    return () => { live = false; };
  }, [files]);
  useEffect(() => () => { if (picking) URL.revokeObjectURL(picking.url); }, [picking]);

  const replace = (old: File, boxes: Box[]) => {
    setError(null);
    blurFile(old, boxes)
      .then((done) => {
        setChecks((m) => new Map(m).set(done, { codes: [], blurred: true }));
        onFiles(files.map((f) => (f === old ? done : f)));
        setPicking(null);
      })
      .catch((e: Error) => setError(t('club.photo_blur_failed', { error: e.message })));
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
                <button type="button" class="cursor-pointer p-0 text-brand-blue underline" onClick={() => pick(f)}>{t('club.photo_blur_region')}</button>
              </span>
              {codes > 0 && (
                <p class="m-0 flex flex-wrap items-center gap-2 rounded-md bg-[#fff4e2] px-2.5 py-1.5 text-[#8a4b00]" role="alert">
                  {t('club.photo_code_found', { n: codes })}
                  <button type="button" class="site-btn site-btn-outline-secondary site-btn-sm" onClick={() => replace(f, c!.codes!)}>
                    {t('club.photo_blur_codes')}
                  </button>
                </p>
              )}
            </li>
          );
        })}
      </ul>
      {picking && (
        <RegionPicker src={picking.url} alt={picking.file.name} t={t}
          onSave={(boxes) => replace(picking.file, boxes)} onCancel={() => setPicking(null)} />
      )}
      {error && <p class="m-0 text-[#a3262e]" role="alert">{error}</p>}
      <label class="flex items-start gap-2 text-body-secondary">
        <input type="checkbox" checked={own} required onChange={(e) => onOwn((e.target as HTMLInputElement).checked)} class="mt-0.5" />
        {t('club.photo_own')}
      </label>
    </div>
  );
}
