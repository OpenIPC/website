// Copy the web fonts the stylesheet references out of node_modules and into
// public/fonts/, where they are served directly and are not fingerprinted.
//
// The files are committed, so this is not part of any build -- neither the
// Dockerfile nor CI runs it, and .dockerignore excludes tools/ from the image
// entirely. Run it by hand (`yarn build:fonts`) after bumping @fontsource or
// bootstrap-icons, and commit whatever changes.
//
// Committed rather than generated because public/ is served by the web server
// without going through Sprockets: a generated file would have to exist before
// assets:precompile in the image build and before the suite renders a page in
// CI, for no benefit -- these change perhaps once a year.
//
// Why self-hosted at all: the stylesheet used to @import fonts.googleapis.com
// and cdn.jsdelivr.net, which made every visitor announce themselves to two
// third parties before the page could paint, and put first paint behind a
// DNS lookup we do not control.

import { mkdirSync, copyFileSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const dest = join(root, 'public', 'fonts')

// Subsets and weights must match the @font-face rules in
// app/assets/stylesheets/_fonts.scss. Copying a file the stylesheet never asks
// for wastes bytes in the repo; missing one that it does ask for is a 404 and a
// silent fallback to the system font.
const SUBSETS = ['latin', 'latin-ext', 'cyrillic']
const FACES = [
  { pkg: '@fontsource/ibm-plex-sans', slug: 'ibm-plex-sans', weights: [400, 500, 600, 700] },
  { pkg: '@fontsource/ibm-plex-mono', slug: 'ibm-plex-mono', weights: [400, 600] },
]

mkdirSync(dest, { recursive: true })

let copied = 0
const missing = []

for (const { pkg, slug, weights } of FACES) {
  for (const subset of SUBSETS) {
    for (const weight of weights) {
      const name = `${slug}-${subset}-${weight}-normal.woff2`
      const from = join(root, 'node_modules', pkg, 'files', name)
      if (!existsSync(from)) { missing.push(name); continue }
      copyFileSync(from, join(dest, name))
      copied++
    }
  }
}

// bootstrap-icons ships its own font next to the stylesheet we import. The
// $bootstrap-icons-font-dir override points it at /fonts, so both formats have
// to land here -- woff2 for everything current, woff as the fallback the
// generated @font-face still lists.
for (const name of ['bootstrap-icons.woff2', 'bootstrap-icons.woff']) {
  const from = join(root, 'node_modules', 'bootstrap-icons', 'font', 'fonts', name)
  if (!existsSync(from)) { missing.push(name); continue }
  copyFileSync(from, join(dest, name))
  copied++
}

if (missing.length) {
  console.error(`copy-fonts: ${missing.length} file(s) not found in node_modules:`)
  for (const name of missing) console.error(`  ${name}`)
  console.error('Run `yarn install` first, or reconcile the lists above with _fonts.scss.')
  process.exit(1)
}

console.log(`copy-fonts: ${copied} file(s) -> public/fonts/`)
