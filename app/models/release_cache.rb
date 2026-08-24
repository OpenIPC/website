# frozen_string_literal: true

require 'digest'
require 'net/http'
require 'uri'

# Fetches a release asset the first time it is wanted and keeps it.
#
# The mirror this replaces downloads every asset the moment it appears, on the
# chance somebody asks. Most never get asked for: fifty-nine distinct SoCs were
# requested in a fortnight, and the twenty most popular account for four fifths
# of that. This fetches what is actually wanted, once.
#
# Files are named for their content, not for the asset:
#
#   blobs/<sha256 hex>                      when upstream publishes a digest
#   blobs/size-<bytes>-<name>               for the twenty-two that do not
#
# which makes "is my copy current?" a question of whether the file is there.
# A rebuilt asset gets a new digest and so a new name; an unchanged one keeps
# its own and is never fetched twice, however many releases republish it.
#
# The twenty-two without a digest are bootloaders, uploaded between 2023 and
# 2025 and unchanged since, because GitHub only began publishing digests later.
# Keying those on the size upstream advertises is what mirror-releases.rb
# already does for them: it cannot notice a change that keeps the same length,
# which for a file that has not moved in two years is a trade worth making.
class ReleaseCache
  class UnknownAsset < StandardError; end
  class Unavailable < StandardError; end

  DOWNLOAD_BASE = 'https://github.com/OpenIPC/firmware/releases/download'

  # github.com redirects to the CDN, which is a different host, so redirects
  # have to be followed -- but only to somewhere GitHub actually serves from.
  ALLOWED_HOSTS = ['github.com', 'objects.githubusercontent.com',
                   'release-assets.githubusercontent.com'].freeze
  MAX_REDIRECTS = 4

  # Measured from this host: 7.9MB in 0.52s, TTFB 0.46s. These are generous
  # against that and still short enough that a wedged upstream cannot hold one
  # of Puma's sixteen threads for long.
  OPEN_TIMEOUT = 5
  READ_TIMEOUT = 60

  LOCK_POLL = 0.1

  class << self
    attr_accessor :lock_timeout

    # Where blobs live. A directory the app writes, unlike the mirror root,
    # which is mounted read-only and belongs to the host's cron.
    # `.presence`, not `fetch`: RELEASE_CACHE_ROOT set but empty would make
    # File.join('', 'blobs') into "/blobs" and write at the root of the
    # filesystem. Set-but-empty is how an unset variable looks in a compose
    # file with a missing interpolation, so it has to mean unset here too.
    def root
      @root ||= ENV['RELEASE_CACHE_ROOT'].presence || Rails.root.join('tmp/release-cache').to_s
    end

    attr_writer :root

    # Seam for tests, so none of this needs the network.
    attr_writer :downloader

    def downloader
      @downloader ||= method(:http_get)
    end

    def path(name)
      new(name).path
    end
  end
  self.lock_timeout = 60

  def initialize(name)
    @name = name
  end

  def path
    entry = lookup
    blob = File.join(blobs_dir, key_for(entry))
    return blob if usable?(blob, entry)

    with_lock(entry) do
      # Whoever held the lock may have been fetching this very blob.
      return blob if usable?(blob, entry)

      fetch(entry, blob)
    end
    blob
  rescue SystemCallError, IOError => e
    # The cache root is a mount, so it can be absent, root-owned, read-only or
    # full -- and the first of those is exactly what a cutover gets wrong.
    # Network and index failures already arrive as Unavailable; local ones
    # should say the same thing rather than becoming a 500, because "try again
    # shortly" is as true of a full disk as of an unreachable GitHub.
    raise Unavailable, "release cache at #{self.class.root}: #{e.class}: #{e.message}"
  end

  private

  # Nothing that follows trusts @name: it has to be something upstream is
  # publishing before it can become a path or a URL.
  def lookup
    entry = ReleaseIndex.current.fetch(@name)
    raise UnknownAsset, "#{@name.inspect} is not in the release index" unless entry

    entry
  rescue ReleaseIndex::Missing => e
    raise Unavailable, "no release index: #{e.message}"
  end

  # Derived from the entry, never from the name alone, so the path component is
  # something this code computed rather than something it was handed.
  def key_for(entry)
    sha = entry.sha256
    return sha if sha&.match?(/\A[0-9a-f]{64}\z/)

    "size-#{entry.bytes.to_i}-#{entry.name.gsub(/[^A-Za-z0-9._-]/, '_')}"
  end

  def usable?(blob, entry)
    File.exist?(blob) && File.size(blob) == entry.bytes.to_i
  end

  def fetch(entry, blob)
    FileUtils.mkdir_p(blobs_dir)
    tmp = File.join(blobs_dir, ".tmp-#{File.basename(blob)}-#{Process.pid}-#{SecureRandom.hex(4)}")
    begin
      sha = self.class.downloader.call(url_for(entry), tmp)
      verify!(entry, tmp, sha)
      File.chmod(0o644, tmp)
      File.rename(tmp, blob)
      # mtime is a property of the asset upstream, not of when we happened to
      # fetch it, so an evicted blob that comes back does not look newer than
      # the images already built from it. Firmware#fresh? compares these.
      stamp(blob, entry)
      Rails.logger.info "release cache: fetched #{entry.name} (#{entry.bytes} bytes)"
    ensure
      FileUtils.rm_f(tmp) if File.exist?(tmp)
    end
  end

  # Built from a constant, the tag and the name -- all three checked before
  # they get here -- rather than following a URL handed over by the API.
  #
  # The name is escaped as a path segment even so. plain_filename? upstream
  # rejects slashes, leading dots and control characters, but it does not
  # reject a space, and an unescaped space makes URI.parse raise rather than
  # fetch. Nothing published today carries one; the day something does is not
  # the day to find out this way.
  def url_for(entry)
    "#{DOWNLOAD_BASE}/#{escape_segment(entry.release)}/#{escape_segment(entry.name)}"
  end

  def escape_segment(value)
    URI::DEFAULT_PARSER.escape(value.to_s, /[^A-Za-z0-9\-._~]/)
  end

  def verify!(entry, tmp, sha)
    actual = File.size(tmp)
    raise Unavailable, "#{entry.name}: got #{actual} bytes, expected #{entry.bytes}" if actual != entry.bytes.to_i

    expected = entry.sha256
    return if expected.nil? || sha == expected

    raise Unavailable, "#{entry.name}: sha256 #{sha} does not match #{expected}"
  end

  def stamp(blob, entry)
    at = Time.parse(entry.updated_at.to_s)
    File.utime(Time.now, at, blob)
  rescue ArgumentError, TypeError
    nil # no usable timestamp upstream; leave whatever the fetch gave it
  end

  def self.http_get(url, dest)
    sha = Digest::SHA256.new
    begin
      uri = URI.parse(url)
    rescue URI::InvalidURIError => e
      raise Unavailable, "#{url}: #{e.message}"
    end
    follow(uri, MAX_REDIRECTS) do |response|
      File.open(dest, 'wb') do |out|
        response.read_body do |chunk|
          sha << chunk
          out << chunk
        end
      end
    end
    sha.hexdigest
  end

  def self.follow(uri, hops, &block)
    raise Unavailable, "too many redirects fetching #{uri}" if hops.negative?
    raise Unavailable, "refusing #{uri.scheme}://#{uri.host}" unless allowed?(uri)

    Net::HTTP.start(uri.host, uri.port, use_ssl: true,
                                        open_timeout: OPEN_TIMEOUT, read_timeout: READ_TIMEOUT) do |http|
      http.request(Net::HTTP::Get.new(uri)) do |response|
        case response
        when Net::HTTPSuccess then return yield(response)
        when Net::HTTPRedirection then return follow(URI.parse(response['location']), hops - 1, &block)
        else raise Unavailable, "#{uri} answered #{response.code}"
        end
      end
    end
  rescue SystemCallError, Net::OpenTimeout, Net::ReadTimeout, OpenSSL::SSL::SSLError,
         URI::InvalidURIError, Net::HTTPBadResponse => e
    raise Unavailable, "#{uri}: #{e.class}: #{e.message}"
  end

  def self.allowed?(uri)
    uri.scheme == 'https' && ALLOWED_HOSTS.include?(uri.host)
  end
  private_class_method :http_get, :follow, :allowed?

  # One fetch per blob, across requests and processes. Separate from the blob
  # so the rename cannot invalidate it, and a non-blocking poll against a
  # deadline because flock has no timeout and a blocked thread here is a thread
  # the whole site loses.
  def with_lock(entry)
    FileUtils.mkdir_p(locks_dir)
    lock = File.open(File.join(locks_dir, "#{key_for(entry)}.lock"), File::CREAT | File::RDWR, 0o644)
    timeout = self.class.lock_timeout
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + timeout
    until lock.flock(File::LOCK_EX | File::LOCK_NB)
      raise Unavailable, "waited #{timeout}s for another request to fetch #{entry.name}" \
        if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep LOCK_POLL
    end
    yield
  ensure
    lock&.flock(File::LOCK_UN)
    lock&.close
  end

  def blobs_dir
    File.join(self.class.root, 'blobs')
  end

  def locks_dir
    File.join(self.class.root, 'locks')
  end
end
