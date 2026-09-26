#!/usr/bin/env bash
#
# Refuse a static bundle that would break the site.
#
#   deploy/static/check-bundle.sh <served-tree> [manifest]
#
# Run three times, deliberately: at the end of build.sh, again as its own CI
# step so a shadowing bundle gets its own red line in the Actions UI rather
# than a line inside a build log, and a third time on the host against the
# extracted tree before the symlink is flipped. The host's copy is master's, so
# today's rules are enforced against any bundle, however old -- which is the
# whole reason a rollback to a six-month-old bundle is a safe thing to offer.
#
# Every rule here is a way the seam can do damage rather than nothing. The seam
# itself cannot fail closed: try_files continues past every miss and ends at
# @fallback, so a bundle that is merely wrong is invisible. A bundle that shadows
# the camera upload, or that carries a directory with no index.html, is not.

set -euo pipefail

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
HERE="$(dirname "$SELF")"
# Absolute, because rule 8 verifies the manifest from inside the tree and a
# relative path does not survive the cd. `check-bundle.sh dist/site` is how CI
# calls it, so this is not a hypothetical.
SITE="$(readlink -f "${1:?usage: check-bundle.sh <served-tree> [manifest]}")"
MANIFEST="$(readlink -f "${2:-$(dirname "$SITE")/MANIFEST}")"
RESERVED="$HERE/reserved-paths"

# Kept in step with the service's locale list by service/deploytest. A
# locale prefix is stripped before a path is matched against the reserved list,
# because /ru/snapshots/x reaches the same route as /snapshots/x and
# would shadow it just as completely.
LOCALES='ru|zh'

fail=0
bad() { printf '\033[31mrefused:\033[0m %s\n' "$*" >&2; fail=1; }

[ -d "$SITE" ] || { printf 'error: %s is not a directory\n' "$SITE" >&2; exit 2; }
[ -f "$RESERVED" ] || { printf 'error: %s is missing\n' "$RESERVED" >&2; exit 2; }

# --- 1. nothing that is not a plain file ------------------------------------
# nginx follows symlinks under root and disable_symlinks is not set, so
# `dist/x -> /etc/passwd` would be a public file on openipc.org.
while IFS= read -r -d '' f; do
  # Not realpath: the link points outside the tree, which is the whole problem,
  # and resolving it would print ../../../etc/passwd instead of its own name.
  bad "/${f#"$SITE"/} is a symlink; nginx would follow it and serve what it points at"
done < <(find "$SITE" -type l -print0)

while IFS= read -r -d '' f; do
  bad "$(realpath --relative-to="$SITE" "$f") is not a regular file"
done < <(find "$SITE" ! -type f ! -type d ! -type l -print0)

# --- 2. no directory in the bundle is empty ---------------------------------
# This used to demand an index.html in every directory. It cannot: a bundle
# with more than one page has intermediate directories that are not pages --
# `ru/` above `ru/donate/index.html`, and the build's own `_astro/` of
# stylesheets and fonts -- and #159 brought both.
#
# The rule was guarding against a 403, and the guard is unnecessary because of
# how the vhost writes the seam. `try_files $uri $uri/index.html @fallback` tests
# its first element as a FILE, since try_files decides file-test versus
# directory-test from whether the literal ends in a slash. A directory misses
# that test, misses index.html too, and falls through to @fallback. Measured by
# deploy/nginx/check-config.sh --seam on nginx 1.26-alpine, which asserts it on
# every run rather than leaving it to this comment:
#
#   /ru/_smoke/      200 static     the page
#   /ru/             not static     the directory above it, not a 403
#   /_astro/app.css  200 static     an asset
#   /_astro/         not static     its directory, not a 403
#
# What is still worth refusing is a directory with nothing under it at all.
# It serves no file, answers nothing, and can only be the residue of a build
# that went wrong -- which is the state a manifest of zero files would not
# otherwise reveal.
while IFS= read -r -d '' d; do
  rel="$(realpath --relative-to="$SITE" "$d")"
  [ "$rel" = "." ] && continue
  [ -n "$(find "$d" -type f -print -quit)" ] \
    || bad "/$rel is empty; a directory that serves nothing should not be in the bundle"
done < <(find "$SITE" -type d -print0)

# --- 3. the root index.html, which is allowed and was not ------------------
# This rule used to refuse one. The reasoning was sound and the conclusion was
# wrong, and it is worth keeping both.
#
# Rails rendered `/` per Accept-Language and declared `Vary: Accept-Language`,
# so one URL answered in three languages, and a file answers in one. nginx
# could only approximate the ranking -- Multilang#browser_locale does RFC 9110
# q-values, honours `q=0` and cuts tags to two letters -- so the rule held `/`
# on Rails and #160 was named as the place the decision would be made.
#
# #165 made it, and not with a map. The choice moved into the browser, which
# is the one party that already knows the answer: `navigator.languages` is the
# visitor's list, in preference order, ranked by the browser. The file says
# English -- which is what 93% of requests to this address get anyway, since
# they send no Accept-Language at all -- and a reader whose browser prefers
# Russian or Chinese is sent to /ru or /zh before the page paints. See the
# script in frontend/apps/site/src/pages/index.astro, and the test that runs
# it rather than a copy of it.
#
# What is checked now is the other direction: the root page has to CARRY that
# script, because a bundle that ships `/` without it serves English to
# everybody and nothing else would notice.
if [ -f "$SITE/index.html" ]; then
  grep -q 'navigator.languages' "$SITE/index.html" \
    || bad "the root index.html does not carry the language redirect; see $(basename "$SELF")"
fi

# --- 4. the smoke page ------------------------------------------------------
if [ -f "$SITE/_smoke/index.html" ]; then
  grep -qE '[0-9a-f]{40}' "$SITE/_smoke/index.html" \
    || bad "_smoke/index.html does not name the commit it was built from"
  grep -q '@@' "$SITE/_smoke/index.html" \
    && bad "_smoke/index.html still has an unsubstituted @@TOKEN@@ in it"
else
  bad "_smoke/index.html is missing; nothing would prove the seam is alive"
fi

# --- 5. nothing shadows a reserved path ---------------------------------------
while IFS= read -r -d '' f; do
  rel="/$(realpath --relative-to="$SITE" "$f")"
  # A directory page is reached at its directory URL, so check that too.
  probe="$rel"
  case "$rel" in */index.html) probe="${rel%/index.html}"; [ -z "$probe" ] && probe=/ ;; esac
  # Strip the locale prefix: /ru/snapshots/x is the same route as /snapshots/x.
  # A "#" delimiter, not "|": $LOCALES is itself an alternation, and with "|"
  # as the delimiter sed ends the pattern at the first locale.
  stripped="$(printf '%s' "$probe" | sed -E "s#^/($LOCALES)(/|\$)#/#")"

  while IFS= read -r rule; do
    case "$rule" in ''|'#'*) continue ;; esac
    for p in $(printf '%s\n%s\n' "$probe" "$stripped" | sort -u); do
      # A rule ending in "/" is a prefix, a rule containing "*" is a glob, and
      # anything else is an exact path. The glob branch leaves $rule unquoted
      # deliberately -- that is what makes */download_full_image cover every
      # vendor and SoC -- which is what SC2254 is about.
      # shellcheck disable=SC2254
      case "$rule" in
        */)   case "$p/" in "$rule"*) bad "$rel shadows the reserved prefix $rule" ;; esac ;;
        *\**) case "$p" in $rule) bad "$rel shadows the reserved path $rule" ;; esac ;;
        *)    [ "$p" = "$rule" ] && bad "$rel shadows the reserved path $rule" ;;
      esac
    done
  done < "$RESERVED"

  # --- 6. nothing strange in the path ---------------------------------------
  case "$rel" in
    *'/../'*|*'/..'|*'//'*) bad "$rel has a path component that is not a name" ;;
    *\\*) bad "$rel contains a backslash" ;;
  esac
  printf '%s' "$rel" | LC_ALL=C grep -qP '[\x00-\x1f]' \
    && bad "$rel contains a control character"

  # --- 7. readable by a worker that is not root -----------------------------
  perm="$(stat -c '%a' "$f")"
  [ $(( 0$perm & 0004 )) -ne 0 ] || bad "$rel is mode $perm; the nginx worker cannot read it"
done < <(find "$SITE" -type f -print0)

while IFS= read -r -d '' d; do
  perm="$(stat -c '%a' "$d")"
  [ $(( 0$perm & 0001 )) -ne 0 ] \
    || bad "/$(realpath --relative-to="$SITE" "$d") is mode $perm; the nginx worker cannot enter it"
done < <(find "$SITE" -type d -print0)

# --- 8. the manifest describes this tree, in both directions ----------------
if [ -f "$MANIFEST" ]; then
  ( cd "$SITE" && sha256sum -c --strict --quiet "$MANIFEST" ) \
    || bad "the manifest does not match the tree: a file changed or is missing"
  listed="$(grep -c . "$MANIFEST" || true)"
  present="$(find "$SITE" -type f | wc -l)"
  [ "$listed" -eq "$present" ] \
    || bad "the manifest lists $listed file(s) and the tree holds $present; something was added after it was written"
else
  bad "no manifest at $MANIFEST"
fi

[ "$fail" -eq 0 ] || { printf '\033[31m==>\033[0m bundle refused\n' >&2; exit 1; }
printf '\033[32m ok\033[0m %s file(s) checked against %s\n' \
  "$(find "$SITE" -type f | wc -l | tr -d ' ')" "$(basename "$RESERVED")"
