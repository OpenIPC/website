#!/usr/bin/env bash
#
# Is the checkout this script runs out of the one the repository has? (#256)
#
# Sourced by deploy.sh and static.sh; not useful on its own.
#
# /srv/www/deploy-src is not a copy of how openipc.org is deployed -- it IS
# what runs. deploy.sh reads docker-compose.yml and legacy-images from beside
# itself, the three installers read their payloads from beside themselves, and
# /usr/local/sbin/openipc-deploy and openipc-static are symlinks into it. A
# command added to the repository does not exist on the host until somebody
# pulls, and a file changed there is stale on the host until the same.
#
# So this warns and never fails anything, deliberately. Refusing to ship a
# working release because a documentation file moved would be the wrong trade.
# Auto-pulling would be worse: a local modification here is a hand-edit
# somebody made to keep production working -- on 2026-09-21 it was #239's
# /rails/shared mount, without which the donate page loses its backer count --
# and discarding that silently is how the fix disappears.
#
# Note the loop that makes this worth a check rather than a habit: a stale
# checkout forces a hand-edit, the hand-edit makes `git pull` refuse, and the
# refusal is what keeps the checkout stale.

# Fills CHECKOUT_ROOT, CHECKOUT_SHA, CHECKOUT_BEHIND and CHECKOUT_DIRTY. Returns 1, silently,
# when the directory is not a git checkout at all -- the normal case for a copy
# rsynced to /tmp to test a branch, where there is nothing to be stale against.
checkout_state() {
  local dir=$1
  git -C "$dir" rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 1

  # The repository root, not the deploy/ subdirectory this script sits in, so
  # the command printed below is the one RESTORE.md and DEV-VALIDATION.md name.
  CHECKOUT_ROOT=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null || printf '%s' "$dir")
  CHECKOUT_SHA=$(git -C "$dir" rev-parse --short HEAD 2>/dev/null || echo unknown)
  # Tracked files only. deploy/.env is untracked, holds PROD_TAG and DEV_TAG,
  # is written by deploy.sh itself and must never be reported as drift.
  CHECKOUT_DIRTY=$(git -C "$dir" status --porcelain 2>/dev/null | grep -v '^??' || true)

  # Bounded: this runs before every deploy and must not be able to hang one.
  # A failed fetch is reported as "cannot tell", never as "current" -- the
  # whole point is that silence has been meaning the wrong thing.
  if timeout 15 git -C "$dir" fetch -q origin master 2>/dev/null; then
    CHECKOUT_BEHIND=$(git -C "$dir" rev-list --count HEAD..FETCH_HEAD 2>/dev/null || echo '?')
  else
    CHECKOUT_BEHIND='?'
  fi
  return 0
}

checkout_warn() {
  local dir=$1
  checkout_state "$dir" || return 0

  if [ "$CHECKOUT_BEHIND" = '?' ]; then
    printf '\033[33m==> cannot tell whether %s is current; the fetch failed\033[0m\n' "$dir" >&2
  elif [ "$CHECKOUT_BEHIND" -gt 0 ]; then
    printf '\033[33m==> %s is %s commit(s) behind master, at %s\033[0m\n' \
      "$dir" "$CHECKOUT_BEHIND" "$CHECKOUT_SHA" >&2
    printf '    This deploy reads docker-compose.yml and legacy-images from there,\n' >&2
    printf '    and the installers read their payloads from there.\n' >&2
    printf '    git -C %s pull --ff-only\n' "$CHECKOUT_ROOT" >&2
  fi

  if [ -n "$CHECKOUT_DIRTY" ]; then
    printf '\033[33m==> %s has local modifications:\033[0m\n' "$dir" >&2
    printf '%s\n' "$CHECKOUT_DIRTY" | sed 's/^/      /' >&2
    printf '    A hand-edit on the host is a change that has not landed. Land it\n' >&2
    printf '    rather than discarding it -- and note that it is also what makes\n' >&2
    printf '    `git pull` refuse, which is what keeps this checkout stale.\n' >&2
  fi
  return 0
}

checkout_report() {
  local dir=$1
  printf 'deploy checkout:\n'
  if ! checkout_state "$dir"; then
    printf '  %s is not a git checkout — running from a copy\n' "$dir"
    return 0
  fi
  printf '  path         %s\n' "$CHECKOUT_ROOT"
  printf '  at           %s\n' "$CHECKOUT_SHA"
  case "$CHECKOUT_BEHIND" in
    0)   printf '  vs master    current\n' ;;
    '?') printf '  vs master    cannot tell; the fetch failed\n' ;;
    *)   printf '  vs master    %s commit(s) behind — git -C %s pull --ff-only\n' \
           "$CHECKOUT_BEHIND" "$CHECKOUT_ROOT" ;;
  esac
  if [ -n "$CHECKOUT_DIRTY" ]; then
    printf '  modified     %s tracked file(s), which is a change that has not landed:\n' \
      "$(printf '%s\n' "$CHECKOUT_DIRTY" | wc -l | tr -d ' ')"
    printf '%s\n' "$CHECKOUT_DIRTY" | sed 's/^/    /'
  fi
  return 0
}
