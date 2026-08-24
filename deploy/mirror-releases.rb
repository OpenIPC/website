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

ROOT = '/srv/github-releases'
LOCK = '/run/lock/openipc-mirror-releases.lock'
TMP_PREFIX = '.tmp-mirror-'

# What the site actually opens. Everything upstream publishes that is not here
# is skipped, and removed if it is already on disk.
#
# An allowlist rather than a list of things to skip: a skip list has to grow
# every time upstream adds a family, and nobody notices it has not until the
# disk is full. The opposite failure -- a family we do want that nobody added
# here -- is the one that bit `plain_filename?` above, so it is made loud
# rather than left to chance: every skipped family is named in the run's log,
# once, so a new one appears in the cron mail the morning it first ships.
#
# boot-*.bin is here although no SoC currently names one. It is the bootloader
# under the name the newer parts use, it costs two megabytes, and guessing that
# upstream will not start using it is the same guess this file already regrets
# making once.
CONSUMED = [
  /\Aopenipc\..+\.tgz\z/,  # Firmware#generate: the kernel and rootfs members
  /\Au-boot-.+\.bin\z/,     # Firmware#generate: written at offset 0
  /\Aboot-.+\.bin\z/,       # the same, for parts that name it this way
  /\A_manifest\.json\z/     # which build this is
].freeze

def consumed?(name)
  CONSUMED.any? { |pattern| name.match?(pattern) }
end

# Files this script makes rather than mirrors.
def ours?(name)
  [STATE_FILE, INDEX_FILE].include?(name) || name.start_with?(TMP_PREFIX)
end

# The leading token, so a run reports "skipping 31 toolchain.*" rather than 31
# lines -- and so a family nobody has seen before is still named.
def family_of(name)
  name[/\A[A-Za-z_]+[.-]/] || name
end

# What we last put on disk, as {name => {size, digest}}. Without it, telling a
# rebuilt asset from the one already here means hashing 4.3 GB every hour.
STATE_FILE = '.mirror-state.json'

# Where the app looks to find out what exists upstream and how to fetch it.
INDEX_FILE = '.index.json'

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
#
# Control characters are the one character rule worth keeping. Every name that
# gets this far is interpolated into a log line, and a name carrying a newline
# could forge entries in the cron log. Rejecting them here means the log lines
# below can stay readable instead of being wrapped in .inspect.
def plain_filename?(name)
  !name.empty? &&
    name == File.basename(name) &&
    !name.start_with?('.') &&
    !name.match?(/[[:cntrl:]]/)
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
# The whole run turns on this one call, and it is the only part of the script
# with no second chance: curl already retries a download three times, but a
# release list that fails takes the index with it. Seen on 2026-08-24 --
#
#   GET https://api.github.com/repos/openipc/firmware/releases?per_page=30:
#   504 - Gateway Time-out
#
# -- which was transient, and the next attempt succeeded. It was not the rate
# limit; that showed 57 of 60 remaining.
#
# A stale index is harmless, because the app reads whatever is on disk and an
# asset that has not changed is still addressed correctly by it. A missing one
# is not, and once the mirror is retired the index is the only thing standing
# between a visitor and a download.
#
# Waits of 5s and 15s: long enough for a gateway hiccup to pass, short enough
# that three attempts cannot overrun the hour between runs.
API_RETRY_WAITS = [5, 15].freeze

def releases_page
  attempt = 0
  begin
    Github.new.repos.releases.list('openipc', 'firmware', per_page: RELEASES_PER_PAGE)
  rescue StandardError => e
    wait = API_RETRY_WAITS[attempt]
    raise if wait.nil?

    attempt += 1
    log "  releases list failed (#{e.class}: #{e.message.to_s.lines.first.to_s.strip}), retrying in #{wait}s"
    sleep wait
    retry
  end
end

def newest_assets
  releases = releases_page
  chosen = {}
  # Names, not counts: an asset published by several releases must be counted
  # once. `chosen` stays exactly the set we intend to mirror, because its keys
  # are what the state file is pruned to and what the disk is pruned against.
  skipped = {}
  releases.each do |release|
    release.assets.each do |asset|
      name = asset['name']
      next if chosen.key?(name) || skipped.key?(name)

      # Before anything is decided about a name, and before any of it reaches a
      # log line. The skip path below reports the family it belongs to, and an
      # asset that never passed this check could put a newline in that line and
      # forge cron log entries -- which is the exact thing plain_filename?
      # exists to prevent, so it has to come first.
      unless plain_filename?(name)
        log "  refusing #{name.inspect} from #{release.tag_name}: not a plain filename"
        next
      end

      unless consumed?(name)
        skipped[name] = family_of(name)
        next
      end

      # The tag goes into the index and from there into a URL path segment.
      # Same reasoning as plain_filename? above, applied to the other half of
      # the address. `tag` rather than release.tag_name is what gets stored
      # below: these are Hashie mashes, and checking one object while keeping
      # another is how `asset.size` came to mean the key count.
      tag = release.tag_name.to_s
      unless tag.match?(/\A[A-Za-z0-9._-]+\z/)
        log "  refusing #{name.inspect}: tag #{tag.inspect} is not a plain tag"
        next
      end

      chosen[name] = {
        name: name,
        url: asset['browser_download_url'],
        size: asset['size'],
        digest: asset['digest'],
        updated_at: asset['updated_at'],
        release: tag
      }
    end
  end

  log format('%d assets published, %d of them consumed here', chosen.size + skipped.size, chosen.size)
  skipped.values.tally
         .sort_by { |_, count| -count }
         .each { |family, count| log format('  skipping %d %s* (nothing here reads them)', count, family) }

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

# What the site is allowed to ask for, and where each thing lives upstream.
#
# The app reads this rather than calling the API itself: the API is
# unauthenticated at 60 requests an hour, shared by every container on the
# host, and a request path that can exhaust that is a request path that can
# take downloads out for the rest of the hour. One call an hour from here
# leaves the whole budget spare.
#
# It is also the allowlist. A name that is not in here is one upstream is not
# publishing, so the app refuses it before it can reach a path or a URL --
# which matters because those names come from a database column an admin can
# edit.
#
# The download URL is deliberately absent. The reader builds it from a constant
# base, the release tag and the asset name, all three of which are checked
# where they are cheap to check -- rather than following a URL handed over by
# the API. Constructing beats validating.
#
# Written .tmp-then-rename so a reader never sees half an index.
def write_index(assets)
  path = File.join(ROOT, INDEX_FILE)
  tmp = "#{path}.tmp"
  index = {
    'generated_at' => Time.now.utc.strftime('%Y-%m-%dT%H:%M:%SZ'),
    'assets' => assets.values.sort_by { |a| a[:name] }.to_h do |a|
      [a[:name], {
        'size' => a[:size],
        'digest' => a[:digest],
        'updated_at' => a[:updated_at],
        'release' => a[:release]
      }]
    end
  }
  File.write(tmp, JSON.pretty_generate(index))

  # Stated rather than inherited from whatever umask cron happens to run with.
  # It is readable today only because the container runs as the same uid this
  # script does, which is a coincidence of the deployment rather than a
  # guarantee -- and firmware images spent years at 0600 for exactly that kind
  # of reason. Set before the rename, unlike public/files: nothing serves this
  # directory, so there is no half-written file to expose, and the index is
  # readable the instant it appears under its real name.
  File.chmod(0o644, tmp)
  File.rename(tmp, path)
  log "wrote #{INDEX_FILE} (#{index['assets'].size} assets)"
end

# Anything in ROOT the site does not read and this script did not write.
#
# Two things end up here. Families skipped on purpose -- 3.1GB of toolchains,
# the kconfig dumps -- and assets upstream has stopped publishing: 52
# openipc-<soc>-<flash>-<release>.bin totalling 393MB, all last written
# 2026-06-13 and carried by no release on the page any more. Without a prune
# the directory only grows, because nothing here has ever taken a file away.
#
# Keyed on what the site reads, deliberately, and not on what upstream still
# offers. The first attempt did the latter, and its dry run showed it taking
# openipc.gk7205v210-nor-lite.tgz -- the seventh most requested SoC on the
# site -- because that build had aged off the first page of releases. A stale
# tarball still flashes a camera; a missing one is a broken download until
# upstream happens to rebuild it. So a consumed asset is never removed here,
# however old it gets.
def prune
  # Sized once, in one pass, and tolerant of a name that is gone by the time we
  # stat it. Nothing else should be writing here -- the lock sees to that --
  # but this runs last, after the fetches, and taking the whole run down over
  # one unreadable file would lose the state file written below it and force
  # 4GB of re-hashing on the next run.
  sizes = {}
  Dir.children(ROOT).each do |name|
    next if consumed?(name) || ours?(name)

    path = File.join(ROOT, name)
    sizes[name] = File.size(path) if File.file?(path)
  rescue SystemCallError => e
    log "  cannot stat #{name.inspect}: #{e.message}"
  end
  return if sizes.empty?

  sizes.keys.group_by { |name| family_of(name) }.sort_by { |_, names| -names.size }.each do |family, names|
    log format('  %s %d %s* (%.1f MB)', DRY_RUN ? 'would remove' : 'removing', names.size, family,
               names.sum { |n| sizes[n] } / 1048576.0)
  end
  return if DRY_RUN

  removed = sizes.keys.count do |name|
    FileUtils.rm_f(File.join(ROOT, name))
    true
  rescue SystemCallError => e
    log "  cannot remove #{name.inspect}: #{e.message}"
    false
  end
  log format('removed %d of %d file(s), %.1f MB', removed, sizes.size,
             sizes.values.sum / 1048576.0)
end

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
  # Every run, and before the fetches: the index is what the app reads, and it
  # is cheap. A run that fails to download anything still leaves it current.
  write_index(assets) unless DRY_RUN

  log "#{wanted.size} to fetch"

  if DRY_RUN
    wanted.each { |a| log "  would fetch #{a[:name]} (#{a[:size]} bytes, #{a[:release]})" }
    prune
    log 'dry run, nothing written'
    exit 0
  end

  fetched = wanted.count { |a| fetch(a, state) }
  log "fetched #{fetched} of #{wanted.size}"

  # Forget assets the org no longer publishes, so the state file tracks the
  # release set rather than growing forever.
  save_state(state.slice(*assets.keys))

  # Last, so that nothing it might do can cost us the digests just written.
  prune

  log 'done'
end
