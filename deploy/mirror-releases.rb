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
require 'json'
require 'open3'

ROOT = '/srv/github-releases'
LOCK = '/run/lock/openipc-mirror-releases.lock'
TMP_PREFIX = '.tmp-mirror-'

# What we last put on disk, as {name => {size, digest}}. Without it, telling a
# rebuilt asset from the one already here means hashing 4.3 GB every hour.
STATE_FILE = '.mirror-state.json'

# Asset names come from the API and are pasted straight into a path, so a name
# is refused rather than sanitised unless it is a plain filename: unchanged by
# basename (which rules out slashes, `..` and `.`) and not starting with a dot
# (which keeps an asset from colliding with the state file or the manifests).
#
# This is a structural test rather than a list of permitted characters. The
# first attempt spelled it as /\A[A-Za-z0-9][...]/ and quietly stopped
# mirroring `_manifest.json`, a real asset that had been on disk since July:
# guessing at the character set upstream is allowed to use is how you refuse
# files you meant to keep.
def plain_filename?(name)
  !name.empty? && name == File.basename(name) && !name.start_with?('.')
end

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

def load_state
  path = File.join(ROOT, STATE_FILE)
  return {} unless File.exist?(path)

  JSON.parse(File.read(path))
rescue JSON::ParserError
  log 'state file unreadable, rebuilding it as files are checked'
  {}
end

def save_state(state)
  path = File.join(ROOT, STATE_FILE)
  tmp = "#{path}.tmp"
  File.write(tmp, JSON.pretty_generate(state))
  File.rename(tmp, path)
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

      unless plain_filename?(name)
        log "  refusing #{name.inspect} from #{release.tag_name}: not a plain filename"
        next
      end

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

# A local file is current when it is both the size and the content the API
# reports. Size is checked first because it is free and settles most cases.
#
# The digest of what we last wrote is remembered rather than recomputed, so a
# quiet run reads no file data at all. The cost is that a file altered outside
# this script, to exactly the same length, is not noticed -- delete the state
# file to force everything to be re-hashed.
def current?(path, asset, state)
  return false unless File.exist?(path)

  size = File.size(path)
  return false unless size.positive? && size == asset[:size]

  expected = asset[:digest]
  return true unless expected&.start_with?('sha256:')

  remembered = state[asset[:name]]
  return remembered['digest'] == expected if remembered && remembered['size'] == size

  # Never hashed, or hashed when the file was a different length: pay for one
  # read now and remember it, so later runs cost nothing.
  actual = "sha256:#{Digest::SHA256.file(path).hexdigest}"
  state[asset[:name]] = { 'size' => size, 'digest' => actual }
  actual == expected
end

# Download beside the target and rename into place. The rename is atomic within
# the directory, so no reader -- and no rsync taking a migration snapshot --
# ever sees a partial file under the real name.
def fetch(asset, state)
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

  actual = "sha256:#{Digest::SHA256.file(tmp).hexdigest}"
  expected = asset[:digest]
  if expected&.start_with?('sha256:') && actual != expected
    log "  FAILED checksum #{asset[:name]} (#{asset[:release]}), keeping the old copy"
    FileUtils.rm_f(tmp)
    return false
  end

  File.rename(tmp, dest)
  state[asset[:name]] = { 'size' => File.size(dest), 'digest' => actual }
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

def manifest_missing?(filename)
  path = File.join(ROOT, filename)
  !File.exist?(path) || File.size(path).zero?
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

MANIFESTS = { '.rootfs.sizes' => 'rootfs', '.kernel.sizes' => 'uImage' }.freeze

with_lock do
  FileUtils.mkdir_p(ROOT)

  # Debris from a run that was killed mid-download, as both overlapping runs
  # were on 2026-08-23.
  stale = Dir.glob(File.join(ROOT, "#{TMP_PREFIX}*"))
  unless stale.empty?
    log "clearing #{stale.size} temporary file(s) from an interrupted run"
    FileUtils.rm_f(stale)
  end

  state = load_state
  assets = newest_assets
  wanted = assets.values.reject { |a| current?(File.join(ROOT, a[:name]), a, state) }
  log "#{assets.size} assets published, #{wanted.size} to fetch"

  if DRY_RUN
    wanted.each { |a| log "  would fetch #{a[:name]} (#{a[:size]} bytes, #{a[:release]})" }
    log 'dry run, nothing written'
    exit 0
  end

  fetched = wanted.count { |a| fetch(a, state) }
  log "fetched #{fetched} of #{wanted.size}"

  # Forget assets the org no longer publishes, so the state file tracks the
  # release set rather than growing forever.
  save_state(state.slice(*assets.keys))

  # Rebuilding a manifest means listing every tarball, which is the expensive
  # part of a run where nothing changed -- but a manifest that is missing or
  # empty has to be rebuilt whether or not anything was downloaded, or the
  # binaries page stays broken until the next night's build happens to land.
  missing = MANIFESTS.keys.select { |m| manifest_missing?(m) }
  if fetched.positive? || !missing.empty?
    log "rebuilding manifests (#{missing.empty? ? 'assets changed' : "#{missing.join(', ')} missing"})"
    MANIFESTS.each { |filename, needle| write_manifest(filename, needle) }
  else
    log 'nothing changed, manifests left alone'
  end

  log 'done'
end
