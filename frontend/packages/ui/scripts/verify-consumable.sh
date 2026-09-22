#!/bin/sh
#
# Prove the package is usable from outside this repository.
#
#   frontend/packages/ui/scripts/verify-consumable.sh
#
# `npm run build` says the bundler is happy. It says nothing about whether the
# `exports` map resolves, whether the declarations it emitted are reachable,
# or whether anything is missing from `files`. This packs the tarball npm
# would publish, installs it into an empty project that shares no node_modules
# with this one, and renders a component through the public entry point.
#
# It is the check #158 asks for in the words "importable from outside the
# repo", and it is the one that catches a package which only works because it
# is being imported from source.

# POSIX sh, so it runs in the same alpine image the rest of the package
# is built in as well as on the CI runner.
set -eu

HERE="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

info 'building'
( cd "$HERE" && npm run build >/dev/null )

info 'packing'
TARBALL="$(cd "$HERE" && npm pack --pack-destination "$WORK" --silent | tail -1)"
[ -f "$WORK/$TARBALL" ] || die "npm pack produced no tarball"

# What is actually inside it, before anything imports it.
tar -tzf "$WORK/$TARBALL" | sed 's|^package/||' | sort > "$WORK/contents"
for required in dist/openipc-ui.js dist/types/index.d.ts dist/tokens.css \
                dist/fonts/IBMPlexSans-Regular.woff2 package.json; do
  grep -qx "$required" "$WORK/contents" || die "the tarball has no $required"
done
# src/ would double the download and let a consumer import past the entry point.
! grep -q '^src/' "$WORK/contents" || die "the tarball ships src/"
! grep -q 'stories' "$WORK/contents" || die "the tarball ships stories"
! grep -q '__fixtures__' "$WORK/contents" || die "the tarball ships fixtures"

info 'installing into an empty project'
mkdir -p "$WORK/consumer"
cd "$WORK/consumer"
cat > package.json <<'JSON'
{ "name": "consumer", "private": true, "type": "module", "version": "0.0.0" }
JSON
npm install --no-audit --no-fund --silent \
  "$WORK/$TARBALL" preact@10.27.2 preact-render-to-string@6.6.2

info 'rendering through the published entry point'
cat > check.mjs <<'JS'
import { renderToString } from 'preact-render-to-string';
import { h } from 'preact';
import { SoCListItem, MainButton, formatUptime, isValidMAC } from '@openipc/ui';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';

const html = renderToString(h(SoCListItem, {
  vendor: 'HiSilicon', group: 'HI3516EV300', model: 'HI3516EV300',
  address: '0x42000000', stage: 'DONE', bootloader: '',
  firmware: 'openipc.hi3516ev300-nor-lite.tgz', featured: true,
  core: null, ai: null, package: null, encoder: null, memory: null,
  href: '/supported-hardware/hisilicon/hi3516ev300',
}));
if (!html.includes('HiSilicon HI3516EV300')) throw new Error('SoCListItem rendered nothing recognisable');
if (!html.includes('/supported-hardware/hisilicon/hi3516ev300')) throw new Error('the href prop was ignored');

if (!renderToString(h(MainButton, { size: 's', caption: 'Flash', clickHandler() {} })).includes('Flash'))
  throw new Error('MainButton rendered nothing recognisable');

if (formatUptime(191400) !== '2d 5h') throw new Error('formatUptime is not the one we built');
if (!isValidMAC('aa:bb:cc:dd:ee:ff')) throw new Error('the validators did not come across');

const require = createRequire(import.meta.url);
const tokens = readFileSync(require.resolve('@openipc/ui/tokens.css'), 'utf8');
if (!tokens.includes('--color-brand-blue')) throw new Error('tokens.css has no tokens');
if (!tokens.includes('./fonts/')) throw new Error('tokens.css still points at the source fonts');

console.log('rendered, typed and styled from a published tarball');
JS
node check.mjs

info 'checking the declarations resolve'
npm install --no-audit --no-fund --silent typescript@6 >/dev/null 2>&1 || \
  npm install --no-audit --no-fund --silent typescript >/dev/null
cat > types.ts <<'TS'
import { SoCListItem, type SoCItem } from '@openipc/ui';
const soc: SoCItem = {
  vendor: 'HiSilicon', group: 'HI3516EV300', model: 'HI3516EV300',
  address: '0x42000000', stage: 'DONE', bootloader: '', firmware: 'x.tgz',
  featured: true, core: null, ai: null, package: null, encoder: null, memory: null,
};
export const used = [SoCListItem, soc];
TS
cat > tsconfig.json <<'JSON'
{ "compilerOptions": { "module": "ESNext", "moduleResolution": "bundler",
  "target": "ES2022", "strict": true, "noEmit": true, "skipLibCheck": true,
  "jsx": "react-jsx", "jsxImportSource": "preact" }, "include": ["types.ts"] }
JSON
npx --no-install tsc -p tsconfig.json

printf '\033[32mok\033[0m — @openipc/ui installs and works outside this repository\n'
