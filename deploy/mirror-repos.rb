#!/usr/bin/ruby
# frozen_string_literal: true

# Keep a local clone of every OpenIPC repository under /srv/github-mirror.
# Nothing serves this -- it is a copy held against losing the GitHub org.
#
# Cron:  0 * * * *  paul  /usr/local/sbin/openipc-mirror-repos
#
# This replaces ~paul/bin/openipc-backup.rb. The fetching is unchanged and
# already incremental; what it lacked was a guard against lapping itself, the
# same fault that let two release-mirror runs overlap and truncate a tarball
# during the 2026-08-23 volume migration.
#
# Three lines were dropped in the move. Two piped git output into `xargs echo
# git ...`, which prints a command rather than running one, so they had no
# effect beyond a fork per repository per hour. Their intent -- create local
# tracking branches for every remote branch, and delete branches gone upstream
# -- is not reinstated here, because turning dead code live is a behaviour
# change and belongs in its own review. The third was a bare `git rebase`,
# which has no upstream to rebase onto in a fresh clone and did nothing but
# fill the cron log with "fatal: invalid upstream 'refs/remotes/origin/master'"
# once per repository per hour; `git pull --all` below does the real work.
#
# Only the first page of the repository listing is fetched, as before -- 30
# repositories, against the 35 directories that have accumulated on disk. The
# rest are no longer refreshed. Widening that is a separate decision: it means
# cloning every repository the org has ever published.

require 'fileutils'
require 'github_api'

ROOT = '/srv/github-mirror'
LOCK = '/run/lock/openipc-mirror-repos.lock'

def log(message)
  warn format('%s  %s', Time.now.utc.strftime('%Y-%m-%dT%H:%M:%SZ'), message)
end

# Non-blocking: skip this hour rather than queue behind the run in progress.
def with_lock
  FileUtils.mkdir_p(File.dirname(LOCK))
  lock = File.open(LOCK, File::CREAT | File::RDWR, 0o644)
  unless lock.flock(File::LOCK_EX | File::LOCK_NB)
    log 'previous run still going, skipping this hour'
    exit 0
  end
  yield
ensure
  lock&.flock(File::LOCK_UN)
  lock&.close
end

with_lock do
  FileUtils.mkdir_p(ROOT)
  github = Github.new
  repos = github.repos.list(user: 'openipc')
  log "#{repos.size} repositories listed"

  cloned = 0
  updated = 0
  failed = []

  repos.each do |repo|
    path = File.join(ROOT, repo.name)

    unless Dir.exist?(path)
      # No shell: repository names and URLs come from the API.
      if system('git', 'clone', '--quiet', repo.clone_url, path)
        cloned += 1
      else
        failed << "#{repo.name} (clone)"
        next
      end
    end

    ok = system('git', '-C', path, 'fetch', '--prune', '--quiet') &&
         system('git', '-C', path, 'pull', '--all', '--quiet')
    ok ? updated += 1 : failed << repo.name
  end

  log "cloned #{cloned}, updated #{updated}"
  log "failed: #{failed.join(', ')}" unless failed.empty?
  log 'done'
end
