#!/usr/bin/ruby
# frozen_string_literal: true

# Mirror the OpenIPC firmware release assets into /srv/github-releases -- the
# directory Soc::RELEASES_ROOT reads and Firmware#generate assembles images
# from.
#
#   mirror-releases.rb            normal run
#   mirror-releases.rb --dry-run  report what would change, download nothing
#
# Cron:  5 * * * *  paul  /usr/local/sbin/openipc-mirror-releases
#
# This replaces ~paul/bin/openipc-backup-releases.rb, which unlinked and
# re-downloaded every asset on every run: about 4 GB an hour, ~100 GB a day,
# for a set that changes once a night. Three consequences, all observed on
# 2026-08-23:
#
#   * A run took longer than the hour between runs, so instances overlapped.
#     Two were live at once during the block-volume migration.
#   * wget wrote straight to the final filename, so every asset spent a moment
#     as a zero-byte hole. A copy taken during that moment captured a
#     zero-byte tarball -- that is how openipc.ssc378de-nor-lite.tgz was lost.
#   * The asset URL and filename were interpolated into a shell command.
#
# It also mirrored the wrong build. Assets were written in release order with
# each overwriting the last, so the *oldest* release on the page won and the
# mirror served firmware a month or two stale. Assets now come from the newest
# release that carries them.
#
# Beware `asset.size`. These are Hashie mashes, so `.size` is Hash#size and
# returns the key count -- 14, identically, for every asset ever published.
# The byte size is `asset['size']`.

require 'digest'
require 'fileutils'
require 'github_api'
require 'open3'

ROOT = '/srv/github-releases'
LOCK = '/run/lock/openipc-mirror-releases.lock'
TMP_PREFIX = '.tmp-mirror-'

# One page of releases, newest first. The rolling `nightly` and `latest` tags
# sort first and carry the current build; older dated nightlies behind them
# fill in assets those two happen not to publish.
RELEASES_PER_PAGE = 30

DRY_RUN = ARGV.include?('--dry-run')

def log(message)
  warn format('%s  %s', Time.now.utc.strftime('%Y-%m-%dT%H:%M:%SZ'), message)
end

# Non-blocking, so an overrun run is skipped rather than queued. Queuing would
# reproduce the overlap this exists to prevent, one hour later.
def with_lock
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

# The newest release that publishes each asset name wins. `list` returns
# releases newest-first, so the first sighting of a name is the freshest.
def newest_assets
  github = Github.new
  releases = github.repos.releases.list('openipc', 'firmware',
                                        per_page: RELEASES_PER_PAGE)
  chosen = {}
  releases.each do |release|
    release.assets.each do |asset|
      name = asset['name']
      next if name.include?('sdk')
      next if chosen.key?(name)

      chosen[name] = {
        name: name,
        url: asset['browser_download_url'],
        size: asset['size'],
        digest: asset['digest'],
        release: release.tag_name
      }
    end
  end
  chosen
end

# A local file is current when it is the size the API reports. Size alone is
# enough to decide *whether* to fetch; the digest below decides whether to keep
# what arrived. A zero-byte local file is never current -- that is what heals
# the holes the old script left behind.
def current?(path, asset)
  return false unless File.exist?(path)

  size = File.size(path)
  size.positive? && size == asset[:size]
end

def digest_ok?(path, asset)
  expected = asset[:digest]
  return true if expected.nil? || !expected.start_with?('sha256:')

  "sha256:#{Digest::SHA256.file(path).hexdigest}" == expected
end

# Download beside the target and rename into place. The rename is atomic within
# the directory, so no reader -- and no rsync taking a migration snapshot --
# ever sees a partial file under the real name.
def fetch(asset)
  dest = File.join(ROOT, asset[:name])
  tmp = File.join(ROOT, "#{TMP_PREFIX}#{asset[:name]}")

  # No shell: asset names and URLs come from the API and are not ours to trust.
  ok = system('curl', '-fsSL', '--retry', '3', '--retry-delay', '2',
              '--max-time', '600', '-o', tmp, asset[:url])
  unless ok
    log "  FAILED download #{asset[:name]} (#{asset[:release]})"
    FileUtils.rm_f(tmp)
    return false
  end

  unless digest_ok?(tmp, asset)
    log "  FAILED checksum #{asset[:name]} (#{asset[:release]}), keeping the old copy"
    FileUtils.rm_f(tmp)
    return false
  end

  File.rename(tmp, dest)
  true
end

# `tar -tv` prints size in the third field. Members are matched the way the
# original did, so .rootfs.sizes keeps the shape BinariesController parses:
# "<tarball> <bytes>", and an absent member leaves the size blank (nil.to_i).
def member_size(tarball, needle)
  out, status = Open3.capture2('tar', '-tvf', tarball)
  return nil unless status.success?

  out.each_line do |line|
    next unless line.include?(needle)
    next if line.include?('md5sum')

    return line.split[2]
  end
  nil
end

def write_manifest(filename, needle)
  path = File.join(ROOT, filename)
  tmp = "#{path}.tmp"
  File.open(tmp, 'w') do |f|
    Dir.glob(File.join(ROOT, 'openipc.*.tgz')).sort.each do |tarball|
      f.puts "#{File.basename(tarball)} #{member_size(tarball, needle)}"
    end
  end
  File.rename(tmp, path)
  log "  wrote #{filename} (#{File.readlines(path).size} entries)"
end

with_lock do
  FileUtils.mkdir_p(ROOT)
  Dir.chdir(ROOT)

  # Debris from a run that was killed mid-download, as both overlapping runs
  # were on 2026-08-23.
  stale = Dir.glob(File.join(ROOT, "#{TMP_PREFIX}*"))
  unless stale.empty?
    log "clearing #{stale.size} temporary file(s) from an interrupted run"
    FileUtils.rm_f(stale)
  end

  assets = newest_assets
  wanted = assets.values.reject { |a| current?(File.join(ROOT, a[:name]), a) }
  log "#{assets.size} assets published, #{wanted.size} to fetch"

  if DRY_RUN
    wanted.each { |a| log "  would fetch #{a[:name]} (#{a[:size]} bytes, #{a[:release]})" }
    log 'dry run, nothing written'
    exit 0
  end

  fetched = wanted.count { |a| fetch(a) }
  log "fetched #{fetched} of #{wanted.size}"

  # Only when something moved: rebuilding these means listing every tarball,
  # which is the expensive part of a run where nothing changed.
  if fetched.positive?
    write_manifest('.rootfs.sizes', 'rootfs')
    write_manifest('.kernel.sizes', 'uImage')
  else
    log 'nothing changed, manifests left alone'
  end

  log 'done'
end
