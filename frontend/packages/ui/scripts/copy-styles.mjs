// tokens.css and fonts.css ship as source: the consumer compiles them with
// their own Tailwind. The @font-face URLs are rewritten to sit next to them in
// dist/. They are two files because a consumer that self-hosts its own copy of
// IBM Plex wants the tokens and not the faces.
import { mkdir, copyFile, readFile, writeFile, readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, '..', 'src');
const dist = join(here, '..', 'dist');

await mkdir(join(dist, 'fonts'), { recursive: true });

for (const name of ['tokens.css', 'fonts.css']) {
  const css = await readFile(join(src, 'styles', name), 'utf8');
  await writeFile(join(dist, name), css.replaceAll('../assets/fonts/', './fonts/'));
}

for (const f of await readdir(join(src, 'assets', 'fonts'))) {
  await copyFile(join(src, 'assets', 'fonts', f), join(dist, 'fonts', f));
}

console.log('tokens.css + fonts.css + fonts -> dist/');
