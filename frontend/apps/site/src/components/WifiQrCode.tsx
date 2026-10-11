import { useState } from 'preact/hooks';
import { Input, MainButton, QrCodeWidget } from '@openipc/ui';
import { wifiPayload, wifiProblems, type WifiFormat } from '../lib/wifi-qr';

/**
 * The Wi-Fi QR code on /tools/qr-code-generator (#160, #440, #441).
 *
 * The camera's qrscan.sh reads both the standard `WIFI:` URI and OpenIPC's
 * `wlanssid=` / `wlanpass=` lines, so the standard one is the default: the
 * same code joins the installer's phone, which proves the SSID and password
 * before the camera ever sees it. The two lines remain an option for firmware
 * that predates the standard form. ../lib/wifi-qr.ts builds both.
 *
 * The code is drawn as the visitor types, from the fields themselves, and
 * only while they describe a network a camera can join; until then the widget
 * shows its placeholder. @openipc/ui encodes it in the page: nothing typed
 * here leaves the browser, which the note beside the form promises.
 *
 * The format switch is a plain fieldset rather than @openipc/ui's Radio: that
 * is a single-row segmented control keyed by its captions, and these captions
 * are long, translated and contain spaces.
 *
 * Every string comes from the page, which reads it from the catalogue, so the
 * form is translated in the same three languages as the copy around it.
 */
export interface WifiQrLabels {
  ssid: string;
  password: string;
  reset: string;
  showPassword: string;
  hidePassword: string;
  formatLegend: string;
  formatStandard: string;
  formatOpenipc: string;
  formatHint: string;
  emptyHint: string;
  tooLong: string;
  /** "QR code for network "{ssid}"". */
  qrLabel: string;
  ssid_required: string;
  ssid_too_long: string;
  password_length: string;
}

const Eye = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
    <circle cx="12" cy="12" r="3" />
  </svg>
);

const EyeOff = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M10.6 5.1A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a17.6 17.6 0 0 1-2.4 3.4M6.6 6.6C3.7 8.4 2 12 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6" />
    <path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" />
    <path d="M2 2l20 20" />
  </svg>
);

export default function WifiQrCode({ labels }: { labels: WifiQrLabels }) {
  const [ssid, setSsid] = useState('');
  const [password, setPassword] = useState('');
  const [format, setFormat] = useState<WifiFormat>('standard');
  const [shown, setShown] = useState(false);
  // "Required" is said once the field has been typed in and emptied, not to
  // a visitor who has only just arrived.
  const [touched, setTouched] = useState(false);

  const problems = wifiProblems(ssid, password);
  const encoded = problems.ssid || problems.password ? '' : wifiPayload(format, ssid, password);
  const ssidError = problems.ssid && (touched || problems.ssid !== 'ssid_required')
    ? labels[problems.ssid]
    : undefined;
  const passwordError = problems.password ? labels[problems.password] : undefined;

  return (
    <div class="grid gap-6 md:grid-cols-2">
      <form
        onSubmit={(event) => event.preventDefault()}
        onReset={() => {
          setSsid('');
          setPassword('');
          setFormat('standard');
          setShown(false);
          setTouched(false);
        }}
      >
        {/*
          `state` is not optional: Input indexes a style table with it, so
          leaving it out is a TypeError during the prerender rather than a
          default.
        */}
        <div class="mb-2">
          <Input
            elemName="wlanssid"
            label={labels.ssid}
            type="text"
            required
            state={ssidError ? 'error' : 'default'}
            errorText={ssidError}
            placeholder="OpenIPC_HotSpot"
            value={ssid}
            onInput={(event: Event) => {
              setSsid((event.target as HTMLInputElement).value);
              setTouched(true);
            }}
          />
        </div>
        <div class="mb-4">
          <Input
            elemName="wlanpass"
            label={labels.password}
            state={passwordError ? 'error' : 'default'}
            errorText={passwordError}
            type={shown ? 'text' : 'password'}
            placeholder="IaMaKo0lHak3r"
            value={password}
            onInput={(event: Event) => setPassword((event.target as HTMLInputElement).value)}
            Icon={shown ? EyeOff : Eye}
            iconPlace="right"
            iconLabel={shown ? labels.hidePassword : labels.showPassword}
            iconTooltip={shown ? labels.hidePassword : labels.showPassword}
            iconClickHandler={() => setShown(!shown)}
          />
        </div>

        <fieldset class="mb-4 rounded-sm border border-grey p-3">
          <legend class="px-1 text-sm">{labels.formatLegend}</legend>
          {([['standard', labels.formatStandard], ['openipc', labels.formatOpenipc]] as const).map(([value, caption]) => (
            <label key={value} class="flex cursor-pointer items-center gap-2 py-1">
              <input
                type="radio"
                name="wifi-format"
                value={value}
                checked={format === value}
                onChange={() => setFormat(value)}
              />
              {caption}
            </label>
          ))}
          <p class="mt-1 mb-0 text-sm text-body-secondary">{labels.formatHint}</p>
        </fieldset>

        <MainButton type="reset" size="s" caption={labels.reset} />
      </form>

      <div class="max-w-sm">
        <QrCodeWidget
          textToCode={encoded}
          label={labels.qrLabel.replace('{ssid}', () => ssid)}
          emptyText={ssidError ?? passwordError ?? labels.emptyHint}
          tooLongText={labels.tooLong}
        />
      </div>
    </div>
  );
}
