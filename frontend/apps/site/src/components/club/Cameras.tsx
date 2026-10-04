/**
 * A member's cameras on the Open Wall, on /club (service/internal/wallstars):
 * each linked camera with what it has earned and why it is or is not earning,
 * and a one-time code that links another when the camera uploads it.
 *
 * The camera's pictures are not shown here: the wall hands them out only over
 * its socket, so each camera links to its own wall page instead.
 */
import { useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { newCameraCode, showOwner, STARS, unlinkCamera, type Camera, type Cameras as Data } from '../../lib/club';
import { Stars } from './parts';

const TONE: Record<Camera['status'], string> = {
  joined: 'bg-[#e3f5ec] text-[#146c3c]',
  counting: 'bg-[#fff4e2] text-[#8a4b00]',
  limit: 'bg-surface-alt text-body-secondary',
  dark: 'bg-[#fbe9ea] text-[#a3262e]',
  silent: 'bg-[#fbe9ea] text-[#a3262e]',
  revoked: 'bg-[#fbe9ea] text-[#a3262e]',
};

export default function Cameras({ data, locale, t, onChange }: {
  data: Data; locale: Locale; t: BoardsT; onChange: () => void;
}) {
  const counting = data.cameras.filter((c) => c.status !== 'limit' && c.status !== 'revoked').length;
  return (
    <section id="cameras" class="grid gap-3 border-b border-hairline px-4 py-4" aria-labelledby="club-cameras">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="club-cameras" class="m-0 text-lg font-semibold">{t('club.cameras_title')}</h2>
        {data.cameras.length > 0 && (
          <span class="text-[13px] text-body-secondary">{t('club.cameras_count', { n: Math.min(counting, data.max_cameras), max: data.max_cameras })}</span>
        )}
      </div>
      {data.cameras.length === 0 && <p class="m-0 text-sm text-body-secondary">{t('club.cameras_empty')}</p>}
      {data.cameras.map((c) => <CameraRow key={c.token} cam={c} max={data.max_cameras} locale={locale} t={t} onChange={onChange} />)}
      <AddCamera code={data.code} max={data.max_cameras} locale={locale} t={t} onChange={onChange} />
    </section>
  );
}

function CameraRow({ cam, max, locale, t, onChange }: { cam: Camera; max: number; locale: Locale; t: BoardsT; onChange: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const [owner, setOwner] = useState(cam.show_owner);
  const hardware = [cam.soc, cam.sensor].filter(Boolean).join(' · ').toUpperCase();
  const status = t(`club.cam_status_${cam.status}`, {
    days: cam.days, need: cam.days + cam.need_days, n: silentDays(cam),
  });
  return (
    <article class="grid gap-x-4 gap-y-2 rounded-lg border border-hairline p-3 sm:grid-cols-[1fr_auto]">
      <div class="grid min-w-0 content-start gap-1">
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="m-0 text-[15px] font-semibold text-ink">{cam.name || t('club.cam_unnamed')}</h3>
          <span class={`whitespace-nowrap rounded-full px-2.5 py-0.5 text-[12px] font-semibold ${TONE[cam.status]}`}>{status}</span>
          {cam.rare && <span class="whitespace-nowrap rounded-full bg-[#ecebff] px-2.5 py-0.5 text-[12px] font-semibold text-[#4434b8]">{t('club.cam_rare')}</span>}
        </div>
        {hardware && <div class="font-mono text-[12px] text-body-secondary">{hardware}{cam.firmware && ` · OpenIPC ${cam.firmware}`}</div>}
        <div class="text-[13px]">
          {t('club.cam_meta', { days: cam.wall_days })}
          {cam.last_frame && <span class="text-body-secondary"> · {t('club.cam_last', { when: ago(cam.last_frame, locale) })}</span>}
        </div>
        {cam.status === 'counting' && (
          <>
            <div class="h-1.5 max-w-[320px] overflow-hidden rounded bg-hairline" role="img"
              aria-label={status}>
              <i class="block h-full rounded bg-brand-blue" style={{ width: `${Math.min(100, (100 * cam.days) / (cam.days + cam.need_days || 1))}%` }} />
            </div>
            <p class="m-0 text-[13px] text-body-secondary">
              {cam.need_days > 0
                ? t('club.cam_counting_help', { join: STARS.join, days: cam.need_days })
                : t('club.cam_counting_age_help', { join: STARS.join, age: cam.need_age })}
            </p>
          </>
        )}
        {cam.status === 'dark' && <p class="m-0 text-[13px] text-body-secondary">{t('club.cam_dark_help')}</p>}
        {cam.status === 'silent' && <p class="m-0 text-[13px] text-body-secondary">{t('club.cam_silent_help', { n: silentDays(cam) })}</p>}
        {cam.status === 'limit' && <p class="m-0 text-[13px] text-body-secondary">{t('club.cam_limit_help', { max })}</p>}
        {cam.status === 'revoked' && <p class="m-0 text-[13px] text-body-secondary">{t('club.cam_revoked_help')}</p>}
      </div>
      <div class="grid content-start gap-1 text-[13px] sm:justify-items-end sm:text-right">
        <Stars n={cam.stars} />
        {cam.status === 'joined' && <span class="text-body-secondary">{t('club.cam_next_star', { n: cam.next_star })}</span>}
        <a href={pathFor(locale, `/open-wall/camera/${cam.token}`)}>{t('club.cam_open')}</a>
        {cam.status !== 'revoked' && (
          <label class="flex cursor-pointer items-center gap-1.5">
            <input type="checkbox" class="accent-brand-blue" checked={owner}
              onChange={(e) => {
                const show = (e.target as HTMLInputElement).checked;
                setOwner(show);
                showOwner(cam.token, show).catch(() => setOwner(!show));
              }} />
            {t('club.cam_show_owner')}
          </label>
        )}
        {confirming
          ? (
            <span class="flex flex-wrap items-center gap-2 sm:justify-end">
              <span class="text-body-secondary">{t('club.cam_unlink_confirm')}</span>
              <button type="button" class="cursor-pointer p-0 font-semibold text-[#a3262e] underline" onClick={() => unlinkCamera(cam.token).then(onChange)}>{t('club.cam_unlink')}</button>
              <button type="button" class="cursor-pointer p-0 text-body-secondary underline" onClick={() => setConfirming(false)}>{t('club.confirm_cancel')}</button>
            </span>
          )
          : <button type="button" class="w-fit cursor-pointer p-0 text-brand-blue underline" onClick={() => setConfirming(true)}>{t('club.cam_unlink')}</button>}
      </div>
    </article>
  );
}

function AddCamera({ code, max, locale, t, onChange }: {
  code: Data['code']; max: number; locale: Locale; t: BoardsT; onChange: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const ask = () => {
    setBusy(true);
    setError(null);
    newCameraCode().then(onChange).catch((e: Error) => setError(e.message)).finally(() => setBusy(false));
  };
  const copy = () => {
    if (!code) return;
    navigator.clipboard?.writeText(code.code).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }, () => {});
  };
  return (
    <div class="grid gap-3 rounded-lg border border-dashed border-[#b7bfef] bg-[#f6f7fe] p-4">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h3 class="m-0 text-base font-semibold text-ink">{t('club.add_title')}</h3>
        <span class="text-[13px] text-body-secondary">{t('club.add_limit', { max })}</span>
      </div>
      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid content-start gap-2">
          {code
            ? (
              <>
                <div class="flex flex-wrap items-center gap-2">
                  <output class="rounded-md border border-hairline bg-white px-3 py-2 font-mono text-xl font-semibold tracking-wider text-ink select-all">{code.code}</output>
                  <button type="button" class="site-btn site-btn-outline-primary site-btn-sm" onClick={copy}>{copied ? t('club.add_copied') : t('club.add_copy')}</button>
                </div>
                {code.blocked && <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-[13px] text-[#8a4b00]" role="alert">{t('club.add_blocked')}</p>}
                <p class="m-0 text-[12.5px] text-body-secondary">{t('club.add_valid', { time: new Date(code.expires_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' }) })}</p>
                <button type="button" class="w-fit cursor-pointer p-0 text-[13px] text-brand-blue underline disabled:opacity-55" disabled={busy} onClick={ask}>{t('club.add_new_code')}</button>
              </>
            )
            : <button type="button" class="site-btn site-btn-primary w-fit disabled:opacity-55" disabled={busy} onClick={ask}>{t('club.add_get_code')}</button>}
          {error && <p class="m-0 text-[13px] text-[#a3262e]" role="alert">{t('club.add_failed', { error })}</p>}
        </div>
        <ol class="m-0 grid list-decimal content-start gap-1.5 ps-5 text-sm">
          <li>{t('club.add_step1')}</li>
          <li>{t('club.add_step2')}</li>
          <li>{t('club.add_step3')}</li>
        </ol>
      </div>
      <p class="m-0 text-[12.5px] text-body-secondary">{t('club.add_privacy')}</p>
    </div>
  );
}

/** Whole days since the camera's last day with a picture (UTC days, as the service counts them). */
function silentDays(cam: Camera, now = Date.now()): number {
  if (!cam.last_day) return 0;
  const today = Date.UTC(new Date(now).getUTCFullYear(), new Date(now).getUTCMonth(), new Date(now).getUTCDate());
  return Math.max(0, Math.round((today - Date.parse(cam.last_day)) / 86_400_000));
}

/** "6 minutes ago", in the page's language. */
export function ago(iso: string, locale: Locale, now = Date.now()): string {
  const seconds = Math.round((Date.parse(iso) - now) / 1000);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [['day', 86_400], ['hour', 3_600], ['minute', 60]];
  for (const [unit, size] of steps) {
    if (Math.abs(seconds) >= size) return rtf.format(Math.round(seconds / size), unit);
  }
  return rtf.format(0, 'minute');
}
