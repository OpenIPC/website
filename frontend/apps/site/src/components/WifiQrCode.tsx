import { useState } from 'preact/hooks';
import { Input, MainButton, QrCodeWidget } from '@openipc/ui';

/**
 * The Wi-Fi QR code on /tools/qr-code-generator (#160).
 *
 * The camera reads a QR code holding two lines, `wlanssid=` and `wlanpass=`,
 * so the encoded text is that exact shape and not a `WIFI:` URI -- which is
 * what a phone would expect and what this camera would not understand.
 *
 * The Rails page pulled qrcode@1.5.1 from jsdelivr on every visit. @openipc/ui
 * encodes it in the page instead: one fewer third party on a page whose
 * neighbour is /privacy, and a code that still renders with JavaScript off
 * once something has been typed -- which, on a form, is never, so the initial
 * state says so rather than showing an empty square.
 *
 * The labels come from the page, which reads them from the catalogue, so the
 * form is translated in the same three languages as the copy around it.
 */
export default function WifiQrCode({ labels }: {
  labels: { password: string; generate: string; reset: string; ssid: string };
}) {
  const [ssid, setSsid] = useState('');
  const [password, setPassword] = useState('');
  const [encoded, setEncoded] = useState('');

  return (
    <div class="grid gap-6 md:grid-cols-2">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setEncoded(`wlanssid=${ssid}\nwlanpass=${password}`);
        }}
        onReset={() => {
          setSsid('');
          setPassword('');
          setEncoded('');
        }}
      >
        {/*
          `state` is not optional: Input indexes a style table with it, so
          leaving it out is a TypeError during the prerender rather than a
          default.
        */}
        <div class="mb-4">
          <Input
            elemName="wlanssid"
            label={labels.ssid}
            type="text"
            state="default"
            placeholder="OpenIPC_HotSpot"
            value={ssid}
            onInput={(event: Event) => setSsid((event.target as HTMLInputElement).value)}
          />
        </div>
        <div class="mb-4">
          <Input
            elemName="wlanpass"
            label={labels.password}
            state="default"
            type="password"
            placeholder="IaMaKo0lHak3r"
            value={password}
            onInput={(event: Event) => setPassword((event.target as HTMLInputElement).value)}
          />
        </div>
        <div class="flex flex-wrap gap-3">
          <MainButton type="submit" size="s" caption={labels.generate} />
          <MainButton type="reset" size="s" caption={labels.reset} />
        </div>
      </form>

      <div class="max-w-sm">
        <QrCodeWidget textToCode={encoded} />
      </div>
    </div>
  );
}
