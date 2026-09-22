#!/usr/bin/env bash
#
# Is the checkout this script runs out of the one the repository has? (#256)
#
# Sourced by deploy.sh and static.sh; not useful on its own.
#
# /srv/www/deploy-src is not a copy of how openipc.org is deployed -- it IS
# what runs. There are two of them since #159: deploy-src on master serves
# production and deploy-src-dev on dev serves dev.openipc.org, so "current"
# means a different branch depending on which one is asking -- which is the
# argument below. deploy.sh reads docker-compose.yml and legacy-images from beside
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

# Fills CHECKOUT_ROOT, CHECKOUT_BRANCH, CHECKOUT_SHA, CHECKOUT_BEHIND,
# CHECKOUT_AHEAD and CHECKOUT_DIRTY. Returns 1, silently,
# when the directory is not a git checkout at all -- the normal case for a copy
# rsynced to /tmp to test a branch, where there is nothing to be stale against.
# The second argument is the branch this checkout is supposed to track:
# master for production, dev for dev. Defaulted so an older caller behaves as
# it always did.
checkout_state() {
  local dir=$1 want=${2:-master}
  git -C "$dir" rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 1

  # The repository root, not the deploy/ subdirectory this script sits in, so
  # the command printed below is the one RESTORE.md and DEV-VALIDATION.md name.
  CHECKOUT_ROOT=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null || printf '%s' "$dir")
  CHECKOUT_SHA=$(git -C "$dir" rev-parse --short HEAD 2>/dev/null || echo unknown)
  CHECKOUT_BRANCH=$(git -C "$dir" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
  # Tracked files only. deploy/.env is untracked, holds PROD_TAG and DEV_TAG,
  # is written by deploy.sh itself and must never be reported as drift.
  CHECKOUT_DIRTY=$(git -C "$dir" status --porcelain 2>/dev/null | grep -v '^??' || true)

  # Bounded: this runs before every deploy and must not be able to hang one.
  # A failed fetch is reported as "cannot tell", never as "current" -- the
  # whole point is that silence has been meaning the wrong thing.
  if timeout 15 git -C "$dir" fetch -q origin "$want" 2>/dev/null; then
    CHECKOUT_BEHIND=$(git -C "$dir" rev-list --count HEAD..FETCH_HEAD 2>/dev/null || echo '?')
    # Both directions. Counting only what master has and this does not would
    # call a FEATURE BRANCH current, and pointing this checkout at a branch to
    # try it on dev is a thing people here actually do -- the runbook says to
    # repoint it at master after the merge, which is a step somebody has to
    # remember. A checkout ahead of master is running deploy code that has not
    # landed, which is the same problem as running code that is out of date.
    CHECKOUT_AHEAD=$(git -C "$dir" rev-list --count FETCH_HEAD..HEAD 2>/dev/null || echo '?')
  else
    CHECKOUT_BEHIND='?'
    CHECKOUT_AHEAD='?'
  fi
  return 0
}

checkout_warn() {
  local dir=$1 want=${2:-master}
  checkout_state "$dir" "$want" || return 0

  if [ "$CHECKOUT_BEHIND" = '?' ]; then
    printf '\033[33m==> cannot tell whether %s is current; the fetch failed\033[0m\n' "$dir" >&2
  else
    if [ "$CHECKOUT_BEHIND" -gt 0 ]; then
      printf '\033[33m==> %s is %s commit(s) behind %s, at %s\033[0m\n' \
        "$dir" "$CHECKOUT_BEHIND" "$want" "$CHECKOUT_SHA" >&2
      printf '    This deploy reads docker-compose.yml and legacy-images from there,\n' >&2
      printf '    and the installers read their payloads from there.\n' >&2
      # `dev` is force-pushed, so --ff-only cannot follow it. Advice that does
      # not work is worse than none: it reads as though the checkout is fine.
      if [ "$want" = dev ]; then
        printf '    git -C %s fetch origin dev && git -C %s reset --hard origin/dev\n' \
          "$CHECKOUT_ROOT" "$CHECKOUT_ROOT" >&2
      else
        printf '    git -C %s pull --ff-only\n' "$CHECKOUT_ROOT" >&2
      fi
    fi
    if [ "$CHECKOUT_AHEAD" -gt 0 ]; then
      printf '\033[33m==> %s carries %s commit(s) %s does not, on %s\033[0m\n' \
        "$dir" "$CHECKOUT_AHEAD" "$want" "$CHECKOUT_BRANCH" >&2
      printf '    This deploy is reading its compose file and its installers from\n' >&2
      printf '    code that has not landed. If a branch was put here to try it on\n' >&2
      printf '    dev, it needs repointing at master once it merges.\n' >&2
    fi
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
  local dir=$1 want=${2:-master}
  printf 'deploy checkout:\n'
  if ! checkout_state "$dir" "$want"; then
    printf '  %s is not a git checkout — running from a copy\n' "$dir"
    return 0
  fi
  printf '  path         %s\n' "$CHECKOUT_ROOT"
  printf '  at           %s (%s)\n' "$CHECKOUT_SHA" "$CHECKOUT_BRANCH"
  if [ "$CHECKOUT_BEHIND" = '?' ]; then
    printf '  vs %-8s cannot tell; the fetch failed\n' "$want"
  elif [ "$CHECKOUT_BEHIND" -eq 0 ] && [ "$CHECKOUT_AHEAD" -eq 0 ]; then
    printf '  vs %-8s current\n' "$want"
  else
    [ "$CHECKOUT_BEHIND" -gt 0 ] && \
      if [ "$want" = dev ]; then
        printf '  vs %-8s %s commit(s) behind — git -C %s reset --hard origin/dev\n' "$want" \
          "$CHECKOUT_BEHIND" "$CHECKOUT_ROOT"
      else
        printf '  vs %-8s %s commit(s) behind — git -C %s pull --ff-only\n' "$want" \
          "$CHECKOUT_BEHIND" "$CHECKOUT_ROOT"
      fi
    [ "$CHECKOUT_AHEAD" -gt 0 ] && \
      printf '  vs %-8s %s commit(s) it does not have — this is running code that has not landed\n' "$want" \
        "$CHECKOUT_AHEAD"
  fi
  if [ -n "$CHECKOUT_DIRTY" ]; then
    printf '  modified     %s tracked file(s), which is a change that has not landed:\n' \
      "$(printf '%s\n' "$CHECKOUT_DIRTY" | wc -l | tr -d ' ')"
    printf '%s\n' "$CHECKOUT_DIRTY" | sed 's/^/    /'
  fi
  return 0
}
