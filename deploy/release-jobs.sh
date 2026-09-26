#!/usr/bin/env bash
#
# The two hourly jobs against GitHub, run from the Go image (#304):
#
#   openipc-publish-release-index [--dry-run|--mirror|--retire-mirror]
#       writes /srv/github-releases/.index.json
#   openipc-mirror-repos
#       keeps a clone of every OpenIPC repository under /srv/github-mirror
#
# One script, installed under both names; the name it is called by is the
# subcommand. They were Ruby scripts run from paul's crontab
# (deploy/publish-release-index.rb, deploy/mirror-repos.rb) and needed a host
# Ruby with the github_api gem. Now they need Docker and the image production
# already runs.
#
# The image is production's Go tag (GO_PROD_TAG in deploy/.env), so the jobs
# move with a deploy and back with a rollback. A tag older than these
# subcommands answers "unknown command" and exits non-zero, into the log.
#
# The container runs as uid 1000, the owner of both directories -- paul, who
# ran the Ruby. The lock directory is shared with the host so the lock is the
# same file the Ruby took: a Go run and a Ruby run can never overlap while
# the two are being swapped.
set -euo pipefail

ENV_FILE=${OPENIPC_DEPLOY_ENV:-/srv/www/deploy-src/deploy/.env}
IMAGE=${OPENIPC_GO_IMAGE:-ghcr.io/openipc/website-go}

case "$(basename "$0")" in
  openipc-publish-release-index)
    cmd=publish-release-index; dir=/srv/github-releases ;;
  openipc-mirror-repos)
    cmd=mirror-repos; dir=/srv/github-mirror ;;
  *)
    echo "$(basename "$0"): call me as openipc-publish-release-index or openipc-mirror-repos" >&2
    exit 2 ;;
esac

tag=${OPENIPC_GO_TAG:-$(sed -n 's/^GO_PROD_TAG=//p' "$ENV_FILE" 2>/dev/null | tail -1)}
if [ -z "$tag" ]; then
  echo "$(date -u +%FT%TZ)  $cmd: no GO_PROD_TAG in $ENV_FILE; nothing run" >&2
  exit 1
fi

# --rm: nothing to keep. --network host: the host's resolver and nothing to
# publish. HOME for git, which wants somewhere to look for a config.
exec docker run --rm --network host \
  -u 1000:1000 -e HOME=/tmp \
  -v "$dir:$dir" -v /run/lock:/run/lock \
  --entrypoint openipc "$IMAGE:$tag" "$cmd" "$@"
