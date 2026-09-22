/**
 * Render every component in the package into one self-contained HTML file.
 *
 *   npm run gallery            -> gallery/index.html
 *
 * Uses vite's Node API rather than a build: ssrLoadModule resolves the
 * `?react` SVG imports and compiles the Tailwind entry with the same plugins
 * the library is built with, so what this draws is what Storybook draws.
 *
 * Fonts are inlined as data: URIs so the file is one artefact that opens from
 * anywhere, with no server and no second request.
 */
import { createServer } from 'vite';
import { renderToString } from 'preact-render-to-string';
import { readFile, writeFile, mkdir, readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const out = join(root, 'gallery');

const server = await createServer({
  root,
  configFile: join(root, 'vite.config.ts'),
  server: { middlewareMode: true },
  appType: 'custom',
  logLevel: 'warn',
});

try {
  const { ENTRIES, GROUPS } = await server.ssrLoadModule('/src/gallery/entry.tsx');
  const cssModule = await server.ssrLoadModule('/src/styles/index.css?inline');
  let css = cssModule.default;

  // The @font-face rules point at ../assets/fonts/; inline the files instead.
  const fontDir = join(root, 'src', 'assets', 'fonts');
  for (const file of await readdir(fontDir)) {
    const b64 = (await readFile(join(fontDir, file))).toString('base64');
    css = css.replaceAll(
      `url('../assets/fonts/${file}')`,
      `url('data:font/woff2;base64,${b64}')`,
    );
  }

  /**
   * Components that import an asset get a Vite dev URL like /src/assets/...,
   * which resolves against a dev server and against nothing at all when the
   * committed page is opened from a file:// path. TeamMember's card
   * background is one. Inline every such reference the render produced.
   */
  const inlineAssetUrls = async (markup) => {
    const types = { svg: 'image/svg+xml', png: 'image/png', jpg: 'image/jpeg',
                    jpeg: 'image/jpeg', webp: 'image/webp', gif: 'image/gif' };
    const refs = [...new Set([...markup.matchAll(/\/src\/assets\/[^"')\s]+/g)].map(m => m[0]))];
    for (const ref of refs) {
      const clean = ref.split('?')[0];
      const ext = clean.split('.').pop().toLowerCase();
      if (!types[ext]) continue;
      const bytes = await readFile(join(root, clean.replace(/^\/+/, '')));
      const uri = `data:${types[ext]};base64,${bytes.toString('base64')}`;
      markup = markup.replaceAll(ref, uri);
    }
    return markup;
  };

  const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

  const item = (x) => `
      <section class="item" id="${esc(x.name)}">
        <div class="item-head">
          <h3 class="item-name">${esc(x.name)}</h3>
          <div class="flags">
            ${x.live ? '<span class="flag flag-live">needs&nbsp;JS</span>' : ''}
            ${x.changed ? '<span class="flag flag-changed">changed</span>' : ''}
          </div>
        </div>
        ${x.note ? `<p class="note">${esc(x.note)}</p>` : ''}
        ${x.changed ? `<p class="changed">${esc(x.changed)}</p>` : ''}
        <div class="stage${x.onBrand ? ' stage-brand' : ''}">${renderToString(x.node)}</div>
      </section>`;

  const section = (group) => `
    <div class="group">
      <h2 id="${esc(group)}">${esc(group)}</h2>
      ${ENTRIES.filter((x) => x.group === group).map(item).join('\n')}
    </div>`;

  const nav = GROUPS.map((g) => `
    <li>
      <a class="nav-group" href="#${esc(g)}">${esc(g)}</a>
      <ul>${ENTRIES.filter((x) => x.group === g).map((x) =>
        `<li><a href="#${esc(x.name)}">${esc(x.name)}${x.changed ? '<i class="dot" aria-label="changed"></i>' : ''}</a></li>`).join('')}</ul>
    </li>`).join('');

  const changedCount = ENTRIES.filter((x) => x.changed).length;

  // Counted, not quoted: the masthead said "135 tests" until it was 171.
  const countStories = async (dir) => {
    let n = 0;
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) n += await countStories(join(dir, entry.name));
      else if (/\.stories\.tsx?$/.test(entry.name)) n++;
    }
    return n;
  };
  const storyCount = await countStories(join(root, 'src'));

  const chrome = `
  /* The chrome is set in the two faces the package itself ships, and takes
     its one accent from the brand token the components are drawn with. */
  :root {
    --ground: #fbfbfd;
    --raised: #ffffff;
    --ink: #14161f;
    --muted: #5c5f70;
    --hairline: #e4e5ed;
    --accent: #4c60d8;
    --flag: #9a6a00;
    --flag-bg: #fdf4e0;
    --flag-line: #ecd9a8;
  }
  :root:not([data-theme="light"]) { }
  @media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
      --ground: #14151c;
      --raised: #1b1d26;
      --ink: #e8e9ef;
      --muted: #9a9dae;
      --hairline: #2b2d38;
      --accent: #8b98ea;
      --flag: #e3b968;
      --flag-bg: #2a2313;
      --flag-line: #4a3c1c;
    }
  }
  :root[data-theme="dark"] {
    --ground: #14151c;
    --raised: #1b1d26;
    --ink: #e8e9ef;
    --muted: #9a9dae;
    --hairline: #2b2d38;
    --accent: #8b98ea;
    --flag: #e3b968;
    --flag-bg: #2a2313;
    --flag-line: #4a3c1c;
  }

  /* EVERY selector below is scoped to the chrome's own classes.
     Tailwind 4 emits the components' utilities inside @layer, and an
     unlayered rule beats every layered rule whatever its specificity -- so a
     bare selector like 'a { color: ... }' here silently repainted the
     .text-white links inside the
     stages. On the brand-blue nav bar that made three of the five labels the
     same colour as the bar. Nothing global, ever. */
  body {
    margin: 0;
    background: var(--ground);
    color: var(--ink);
    font-family: 'IBM Plex Sans', ui-sans-serif, system-ui, sans-serif;
    font-size: 15px;
    line-height: 1.55;
  }
  .masthead a, .nav a { color: var(--accent); }
  .masthead :focus-visible, .nav :focus-visible {
    outline: 2px solid var(--accent); outline-offset: 2px;
  }

  .masthead { max-width: 78rem; margin: 0 auto; padding: 3rem 1.5rem 0; }
  .masthead h1 {
    font-family: 'Share Tech Mono', ui-monospace, monospace;
    font-size: clamp(1.6rem, 4vw, 2.2rem);
    letter-spacing: -0.01em; margin: 0 0 .5rem; text-wrap: balance;
  }
  .lede { max-width: 42rem; color: var(--muted); margin: 0 0 1rem; }
  .lede + .lede { margin-top: -.35rem; }
  .counts {
    display: flex; flex-wrap: wrap; gap: .4rem 1.25rem;
    font-family: 'Share Tech Mono', ui-monospace, monospace;
    font-size: 12.5px; color: var(--muted); text-transform: uppercase;
    letter-spacing: .09em; padding-top: .35rem;
    border-top: 1px solid var(--hairline); margin-top: 1.25rem;
  }
  .counts b { color: var(--ink); font-weight: 400; font-variant-numeric: tabular-nums; }

  .wrap { display: grid; grid-template-columns: 14rem minmax(0, 1fr); gap: 2.5rem;
          max-width: 78rem; margin: 0 auto; padding: 1.75rem 1.5rem 6rem; }

  .nav { position: sticky; top: 1.5rem; align-self: start;
         max-height: calc(100vh - 3rem); overflow: auto; font-size: 13px; }
  .nav ul { list-style: none; margin: 0; padding: 0; }
  .nav > ul > li { margin-bottom: 1rem; }
  .nav-group { display: block; font-weight: 600; color: var(--ink); text-decoration: none;
               font-size: 11.5px; text-transform: uppercase; letter-spacing: .1em;
               padding-bottom: .3rem; }
  .nav li li a { display: flex; align-items: center; gap: .4rem; color: var(--muted);
                 text-decoration: none; padding: .1rem 0; }
  .nav li li a:hover, .nav-group:hover { color: var(--accent); }
  .dot { width: 5px; height: 5px; border-radius: 50%; background: var(--flag); flex: none; }

  .group > h2 { font-size: 12px; text-transform: uppercase; letter-spacing: .12em;
                color: var(--muted); font-weight: 600;
                border-bottom: 1px solid var(--hairline);
                padding-bottom: .5rem; margin: 2.75rem 0 1.5rem; }
  .group:first-child > h2 { margin-top: 0; }

  .item { margin-bottom: 2.25rem; }
  .item-head { display: flex; align-items: baseline; gap: .75rem; flex-wrap: wrap; }
  .item-name { font-family: 'Share Tech Mono', ui-monospace, monospace;
               font-size: 1rem; font-weight: 400; margin: 0; }
  .flags { display: flex; gap: .4rem; }
  .flag { font-family: 'Share Tech Mono', ui-monospace, monospace; font-size: 10.5px;
          letter-spacing: .06em; text-transform: uppercase; padding: .05rem .4rem;
          border-radius: 2px; white-space: nowrap; }
  .flag-live { color: var(--muted); border: 1px solid var(--hairline); }
  .flag-changed { color: var(--flag); background: var(--flag-bg); border: 1px solid var(--flag-line); }
  .note { margin: .2rem 0 0; font-size: 13px; color: var(--muted); max-width: 46rem; }
  .changed { margin: .2rem 0 0; font-size: 13px; color: var(--flag);
             max-width: 46rem; padding-left: .6rem; border-left: 2px solid var(--flag-line); }

  /* The stage stays light in both themes. These components are a light-only
     design system -- there are no dark variants to switch to -- so inverting
     the page around them would show colours the library does not have. */
  .stage { margin-top: .75rem; background: #ffffff; color: #111111;
           border: 1px solid var(--hairline); border-radius: 4px;
           padding: 1.5rem; overflow-x: auto;
           /* body's 15px/1.55 would otherwise cascade into the components,
              which are drawn against the 16px root Tailwind assumes. */
           font-size: 1rem; line-height: 1.5; }
  .stage-brand { background: #4c60d8; }

  @media (max-width: 62rem) {
    .wrap { grid-template-columns: 1fr; gap: 1.5rem; }
    .nav { position: static; max-height: none;
           border-bottom: 1px solid var(--hairline); padding-bottom: 1rem; }
    .nav > ul { display: grid; grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr)); gap: 1rem; }
  }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
`;

  const head = `<title>@openipc/ui</title>
<style>${css}</style>
<style>${chrome}</style>`;

  const body = `
  <div class="masthead">
    <h1>@openipc/ui</h1>
    <p class="lede">Every component lifted out of <b>OpenIPC/fancyweb-ng</b>, server-rendered
      from the extraction branch for <a href="https://github.com/OpenIPC/website/issues/158">#158</a>.
      Nothing here is restyled: each one is drawn with the package's own tokens and typefaces,
      on white, because it is a light-only design system with no dark variants to switch to.</p>
    <p class="lede">This page runs no JavaScript, so anything interactive shows its first paint only.
      An amber note says what the extraction changed about a component and why.</p>
    <p class="counts">
      <span><b>${ENTRIES.length}</b> components</span>
      <span><b>${changedCount}</b> changed on the way out</span>
      <span><b>${ENTRIES.filter((x) => x.live).length}</b> need JavaScript</span>
      <span><b>${storyCount}</b> Storybook stories</span>
    </p>
  </div>
  <div class="wrap">
    <nav class="nav" aria-label="Components"><ul>${nav}</ul></nav>
    <main>${GROUPS.map(section).join('\n')}</main>
  </div>`;

  const inlined = await inlineAssetUrls(body);

  const html = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
${head}
</head>
<body>${inlined}</body>
</html>`;

  await mkdir(out, { recursive: true });
  await writeFile(join(out, 'index.html'), html);

  // The same page without the document wrapper, for publishing as an Artifact,
  // which supplies its own <head> and <body>.
  await writeFile(join(out, 'artifact.html'), `${head}\n${inlined}\n`);

  console.log(`gallery/index.html — ${ENTRIES.length} components, ${(html.length / 1024).toFixed(0)} kB`);
  console.log(`gallery/artifact.html — the same page, without the document wrapper`);
} finally {
  await server.close();
}
