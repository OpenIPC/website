// tokens.css ships as source: the consumer compiles it with their own
// Tailwind. The @font-face URLs are rewritten to sit next to it in dist/.
import { mkdir, copyFile, readFile, writeFile, readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, '..', 'src');
const dist = join(here, '..', 'dist');

await mkdir(join(dist, 'fonts'), { recursive: true });

const css = await readFile(join(src, 'styles', 'tokens.css'), 'utf8');
await writeFile(join(dist, 'tokens.css'), css.replaceAll('../assets/fonts/', './fonts/'));

for (const f of await readdir(join(src, 'assets', 'fonts'))) {
  await copyFile(join(src, 'assets', 'fonts', f), join(dist, 'fonts', f));
}

console.log('tokens.css + fonts -> dist/');
