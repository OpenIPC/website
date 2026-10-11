#!/usr/bin/env bash
#
# What happened to a share link, from the relay's own log: the reference a
# guest's failure screen shows ("Ref: <share>/<session> · hh:mm:ssZ") is the key.
#
#   deploy/share-why.sh 030c071fede86fb8                   # the share's whole timeline
#   deploy/share-why.sh 030c071fede86fb8/3f2a9c0e1b7d4a55  # one guest session
#   SINCE="-2 days" deploy/share-why.sh <ref>              # further back (default 7 days)
#
# Each line is one of the relay's records (sharerelay/relay.go):
#   registered  the camera put the link up: its address, and what it says it
#               is (soc, sensor, ram, majestic, firmware) when it says
#   session     one guest's attempt: refused | connected | error | busy |
#               closed | no-answer | page-left | relay-closed, the camera's
#               reason, how long it took, the guest's address and browser, and
#               how many other guests the camera held at that moment
#   page said   the guest's page, for an attempt that failed: its verdict
#               (where it stopped, the sentence shown, the path) -- the
#               reason for refusals that travel inside the encrypted link
#   ended       the link came down: revoked, expired
#
# The camera's own view of a refusal -- its viewer budget, its memory -- is in
# the session's "detail" when its majestic sends one.
#
# The relay that serves the share names runs on the openipc.kz host (see
# deploy/SHARE.md); its journal is read there over ssh.
set -euo pipefail

HOST="${KZ_NGINX_HOST:-194.238.42.216}"
USER="${KZ_NGINX_USER:-ubuntu}"
PORT="${KZ_NGINX_PORT:-22}"
SINCE="${SINCE:--7 days}"

ref="${1:-}"
ref="${ref%% *}"   # "<share>/<session> · 10:18:32Z" pasted whole
share="${ref%%/*}"
session=""
[[ "$ref" == */* ]] && session="${ref#*/}"
[[ "$session" == "-" ]] && session=""
if ! [[ "$share" =~ ^[0-9a-f]{16}$ ]]; then
  echo "usage: $0 <share id>[/<session id>]   (the Ref line of a guest's failure screen)" >&2
  exit 2
fi
if [[ -n "$session" && ! "$session" =~ ^[0-9a-f]{16}$ ]]; then
  echo "not a session id: $session" >&2
  exit 2
fi

# SHARE_WHY_LOG=<file>: a saved journal (journalctl -o cat) instead of the host.
journal() {
  if [[ -n "${SHARE_WHY_LOG:-}" ]]; then
    grep -F "\"$share\"" "$SHARE_WHY_LOG" || true
  else
    ssh -p "$PORT" -o BatchMode=yes "$USER@$HOST" \
      "sudo journalctl -u openipc-share -o cat --since '$SINCE' --no-pager | grep -F '\"$share\"' || true"
  fi
}

journal |
  SESSION="$session" python3 -c "$(cat <<'PY'
import json, os, sys

want = os.environ.get("SESSION")
pad = " " * 21
rows = 0
matched = False  # a line for the session asked about: the relay's or the page's
outcome = False  # the relay's own record of it
for line in sys.stdin:
    try:
        r = json.loads(line)
    except ValueError:
        continue
    msg = r.get("msg", "")
    if not msg.startswith("share: "):
        continue
    kind = msg[len("share: "):]
    # One attempt asked about: its session line, and the share's registration
    # and end around it -- not the share's other attempts, the refused ones
    # (which carry no session) included.
    if want and kind in ("session", "page said"):
        if r.get("session") != want:
            continue
        matched = True
        outcome = outcome or kind == "session"
    rows += 1
    at = r.get("time", "")[:19].replace("T", " ")
    cam = " ".join("%s=%s" % (k[4:], v) for k, v in sorted(r.items()) if k.startswith("cam_"))
    cam = "  [%s]" % cam if cam else ""
    if kind == "registered":
        moved = "  (moved)" if r.get("moved") else ""
        print("%s  registered  camera %s  until %s%s%s" % (at, r.get("remote"), r.get("expires"), moved, cam))
    elif kind == "ended":
        print("%s  ended       %s  (pending %s, connected %s)" % (at, r.get("why"), r.get("pending"), r.get("connected")))
    elif kind == "session":
        took = " after %s ms" % r["ms"] if r.get("ms") is not None else ""
        print("%s  session     %s  %s%s" % (at, r.get("session", "-"), r.get("outcome"), took))
        if r.get("why"):
            print("%swhy: %s" % (pad, r["why"]))
        print("%sguest %s  %s" % (pad, r.get("guest"), (r.get("ua") or "")[:110]))
        if "camera" in r:
            print("%scamera %s, also holding %s connected / %s starting%s" % (
                pad, r.get("camera") or "offline", r.get("others_connected"), r.get("others_pending"), cam))
        if r.get("detail"):
            print("%sdetail: %s" % (pad, r["detail"]))
    elif kind == "page said":
        print("%s  page said   %s  stopped at: %s" % (at, r.get("session"), r.get("where")))
        if r.get("why"):
            print("%swhy: %s" % (pad, r["why"]))
        if r.get("path"):
            print("%spath: %s" % (pad, r["path"]))
if want and matched and not outcome:
    print("%s(the relay's own record of this attempt is not in the log yet: it is written when the attempt ends)" % pad)
if want and not matched:
    print("no attempt %s on that share in the relay log (try SINCE=\"-30 days\", or the share alone)" % want, file=sys.stderr)
    sys.exit(1)
if not rows:
    print('nothing in the relay log for that reference (try SINCE="-30 days")', file=sys.stderr)
    sys.exit(1)
PY
)"
