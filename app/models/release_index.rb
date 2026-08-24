# frozen_string_literal: true

require 'json'

# What upstream is publishing, as written by deploy/mirror-releases.rb into
# Soc::RELEASES_ROOT/.index.json once an hour.
#
# Read from a file rather than from the API on purpose. The GitHub API is
# unauthenticated here, 60 calls an hour shared by every container on the host,
# so a request path that can spend that budget is a request path that can take
# downloads out for the rest of the hour. Reading a 65KB file costs nothing and
# cannot fail that way.
#
# It is also the allowlist. Asset names arrive from the uboot_filename and
# linux_filename columns, which an admin can edit, and end up in a path and a
# URL; a name upstream is not publishing is refused here before it becomes
# either.
class ReleaseIndex
  class Missing < StandardError; end

  # `bytes` rather than `size`: a Struct member called size shadows
  # Struct#size, and this repository has already been bitten once by a `.size`
  # that meant something other than the byte count -- mirror-releases.rb still
  # carries the warning that Hashie's asset.size returns 14 for every asset
  # ever published. Not a name worth reusing.
  Entry = Struct.new(:name, :bytes, :digest, :updated_at, :release, keyword_init: true) do
    # sha256 when upstream publishes one. Twenty-two assets carry no digest --
    # all bootloaders, uploaded between 2023 and 2025, none rebuilt since,
    # because GitHub only began publishing digests later.
    def sha256
      digest.to_s.delete_prefix('sha256:') if digest.to_s.start_with?('sha256:')
    end
  end

  # Re-read when the file on disk has moved on. The mirror renames it into
  # place, so a reader either sees the whole of one version or the whole of the
  # previous one, never a mixture.
  class << self
    def current
      path = index_path
      raise Missing, "#{path} is not there" unless File.exist?(path)

      stamp = [File.mtime(path).to_f, File.size(path)]
      @current = nil if @stamp != stamp
      @stamp = stamp
      @current ||= new(JSON.parse(File.read(path)))
      warn_if_stale(@current)
      @current
    rescue JSON::ParserError, SystemCallError, IOError => e
      # Everything from here reaches the caller as "no usable index", which is
      # what ReleaseCache turns into Unavailable. Without the IO arms, a file
      # that vanishes between the exist? and the read -- the mirror renames a
      # new one into place every hour -- escapes as an unhandled error instead.
      raise Missing, "#{path} is unreadable: #{e.class}: #{e.message}"
    end

    # An index frozen by a cron that stopped running looks exactly like a
    # current one: every name it lists still resolves, and nothing new ever
    # appears. `generated_at` has been written into the file all along and
    # nothing has ever read it.
    #
    # Warned about rather than refused. A stale index is still the best
    # information there is, and every download it can answer still works; what
    # is wanted is for somebody to notice. Throttled, because this is reached
    # several times per request.
    STALE_AFTER = 6.hours
    WARN_EVERY = 1.hour

    def warn_if_stale(index)
      raw = index.generated_at
      return if raw.blank?

      generated = Time.zone.parse(raw.to_s)
      return warn_once("release index: generated_at #{raw.inspect} is not a timestamp") if generated.nil?
      return if generated > STALE_AFTER.ago

      warn_once("release index: generated #{generated.utc.iso8601}, " \
                "#{((Time.current - generated) / 3600).round} hours ago -- " \
                'is the publisher still running?')
    rescue ArgumentError, TypeError => e
      warn_once("release index: generated_at is unreadable: #{e.class}: #{e.message}")
    end

    def warn_once(message)
      return if @warned_at && @warned_at > WARN_EVERY.ago

      @warned_at = Time.current
      Rails.logger.warn message
      nil
    end

    def index_path
      File.join(ENV.fetch('RELEASE_INDEX_ROOT', Soc::RELEASES_ROOT), '.index.json')
    end

    def reset!
      @current = nil
      @stamp = nil
      @warned_at = nil
    end
  end

  def initialize(document)
    @generated_at = document['generated_at']
    @assets = document.fetch('assets', {})
    @aliases = document.fetch('aliases', {})
  end

  attr_reader :generated_at

  # The board upstream actually builds for a chip it no longer builds on its
  # own. GK7205V210 is firmware-identical to GK7205V200 and XM550 to XM530, so
  # since 2026-06-07 only one of each pair is built and the other is served from
  # it; hi3516cv610, hi3516cv608 and hi3516dv500 are the same arrangement.
  #
  # A chip with no entry is its own board, which is the answer for all but a
  # handful. One hop only: the map is flat upstream, and following a chain would
  # turn a bad entry into a loop inside a page render.
  def canonical_board(board)
    @aliases[board] || board
  end

  def fetch(name)
    row = @assets[name]
    return nil unless row

    Entry.new(name: name, bytes: row['size'], digest: row['digest'],
              updated_at: row['updated_at'], release: row['release'])
  end

  def names
    @assets.keys
  end

  def size
    @assets.size
  end

  # Which editions upstream publishes for a board and flash type, read out of
  # the asset names rather than from a list kept here. A variant this site has
  # never heard of therefore becomes offerable the hour it first appears, which
  # is how `neo` should have arrived instead of shipping to seven boards that
  # nothing here could reach.
  ASSET_NAME = /\Aopenipc\.(.+)-(nor|nand)-([a-z0-9]+)\.tgz\z/

  def releases_for(board, flash_type)
    builds[[board.to_s, flash_type.to_s]] || []
  end

  private

  # Built once per index document, which is itself memoised until the file on
  # disk moves on. One pass over ~200 names.
  def builds
    @builds ||= @assets.each_key.with_object({}) do |name, map|
      match = ASSET_NAME.match(name)
      next unless match

      (map[[match[1], match[2]]] ||= []) << match[3]
    end
  end

end
