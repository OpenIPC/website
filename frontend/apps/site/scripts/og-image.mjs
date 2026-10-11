/**
 * Draw a page's social card: the image a link to it shows in Telegram, on
 * social sites and in chats (og:image, #442).
 *
 *   node scripts/og-image.mjs [out-dir]     (default: public/og)
 *
 * One 1200x630 PNG per locale, because the card carries the page's name. The
 * cards are committed: this is run when a card changes, not by the build.
 *
 * The page is laid out in HTML, in the site's own IBM Plex from public/fonts
 * (Chinese falls back to the system's Noto Sans CJK), and photographed with a
 * headless Chromium -- `CHROMIUM` names it, or `chromium` on the PATH. The QR
 * code on it is a real one, encoded by the same qrcodegen the page uses, at
 * the high error-correction level that lets it still scan with the Wi-Fi
 * glyph over its centre.
 */
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { transformSync } from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const site = resolve(here, '..');
const outDir = resolve(process.argv[2] ?? join(site, 'public/og'));

// qrcodegen is TypeScript with parameter properties, which Node's type
// stripping refuses; esbuild turns it into a module Node can import.
const work = mkdtempSync(join(tmpdir(), 'og-image-'));
const qrSource = readFileSync(join(site, '../../packages/ui/src/vendor/qrcodegen.ts'), 'utf8');
writeFileSync(join(work, 'qrcodegen.mjs'), transformSync(qrSource, { loader: 'ts', format: 'esm' }).code);
const { QrCode, QrSegment, Ecc } = await import(pathToFileURL(join(work, 'qrcodegen.mjs')).href);

const CARDS = {
  'qr-code-generator': {
    payload: 'WIFI:S:OpenIPC;T:WPA;P:project2021;;',
    text: {
      en: { title: 'Wi-Fi QR Code Generator', tagline: 'Show it to an OpenIPC camera — it joins the network. Phones too.' },
      ru: { title: 'Генератор QR\u2011кода для Wi\u2011Fi', tagline: 'Покажите его камере с OpenIPC — она подключится к сети. Телефон тоже.' },
      zh: { title: 'Wi-Fi 二维码生成器', tagline: '展示给 OpenIPC 摄像头，即可连接网络。手机同样适用。' },
    },
    url: 'openipc.org/tools/qr-code-generator',
  },
};

function qrSvg(text) {
  const qr = QrCode.encodeSegments(QrSegment.makeSegments(text), Ecc.HIGH, 1, 40, -1, true);
  const border = 2;
  const size = qr.size + border * 2;
  const d = [];
  for (let y = 0; y < qr.size; y++) {
    for (let x = 0; x < qr.size; x++) {
      if (qr.getModule(x, y)) d.push(`M${x + border},${y + border}h1v1h-1z`);
    }
  }
  // The glyph covers about 9% of the code; level H restores up to 30%.
  const c = size / 2;
  const r = size * 0.15;
  const wifi = `
    <circle cx="${c}" cy="${c}" r="${r}" fill="#fff"/>
    <g fill="none" stroke="#0f1422" stroke-width="${r * 0.16}" stroke-linecap="round">
      <path d="M${c - r * 0.62},${c - r * 0.08} a${r * 0.88},${r * 0.88} 0 0 1 ${r * 1.24},0"/>
      <path d="M${c - r * 0.38},${c + r * 0.16} a${r * 0.54},${r * 0.54} 0 0 1 ${r * 0.76},0"/>
    </g>
    <circle cx="${c}" cy="${c + r * 0.42}" r="${r * 0.12}" fill="#0f1422"/>`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges">
    <rect width="100%" height="100%" fill="#fff"/><path d="${d.join(' ')}" fill="#0f1422"/>${wifi}</svg>`;
}

const fontFaces = [400, 600, 700].map((weight) => ['latin', 'cyrillic'].map((subset) => `
  @font-face { font-family: 'IBM Plex Sans'; font-weight: ${weight};
    src: url('${pathToFileURL(join(site, `public/fonts/ibm-plex-sans-${subset}-${weight}-normal.woff2`)).href}') format('woff2'); }`).join('')).join('');
const logo = readFileSync(join(site, 'src/assets/openipc-logo-white.svg'), 'utf8');

function page({ title, tagline }, card) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>
    ${fontFaces}
    html, body { margin: 0; }
    body {
      width: 1200px; height: 630px; box-sizing: border-box; overflow: hidden;
      display: flex; align-items: center; gap: 64px; padding: 0 80px;
      background-color: #0f1422;
      background-image: radial-gradient(rgba(255,255,255,0.09) 1.2px, transparent 1.2px);
      background-size: 24px 24px; background-position: 12px 12px;
      color: #fff; font-family: 'IBM Plex Sans', 'Noto Sans CJK SC', sans-serif;
    }
    .text { flex: 1; display: flex; flex-direction: column; gap: 28px; }
    .logo svg { width: 232px; height: 64px; display: block; }
    h1 { margin: 0; font-size: 64px; line-height: 1.08; font-weight: 700; letter-spacing: -0.01em; }
    p { margin: 0; font-size: 28px; line-height: 1.35; color: #c9d1e0; }
    .url { font-size: 22px; color: #8a93a6; }
    .code { flex: none; width: 400px; height: 400px; padding: 20px; background: #fff; border-radius: 24px; box-sizing: border-box; }
    .code svg { width: 100%; height: 100%; display: block; }
  </style></head><body>
    <div class="text">
      <div class="logo">${logo}</div>
      <h1>${title}</h1>
      <p>${tagline}</p>
      <p class="url">${card.url}</p>
    </div>
    <div class="code">${qrSvg(card.payload)}</div>
  </body></html>`;
}

const chromium = process.env.CHROMIUM ?? 'chromium';
mkdirSync(outDir, { recursive: true });
try {
  for (const [name, card] of Object.entries(CARDS)) {
    for (const [locale, text] of Object.entries(card.text)) {
      const html = join(work, `${name}.${locale}.html`);
      writeFileSync(html, page(text, card));
      const out = join(outDir, `${name}.${locale}.png`);
      execFileSync(chromium, [
        '--headless', '--disable-gpu', '--hide-scrollbars', '--force-device-scale-factor=1',
        '--window-size=1200,630', '--default-background-color=0f1422ff',
        `--screenshot=${out}`, pathToFileURL(html).href,
      ], { stdio: 'ignore' });
      console.log(`wrote ${out}`);
    }
  }
} finally {
  rmSync(work, { recursive: true, force: true });
}
