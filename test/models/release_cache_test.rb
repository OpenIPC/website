# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

class ReleaseCacheTest < ActiveSupport::TestCase
  PAYLOAD = ('OpenIPC firmware payload ' * 40).b
  SHA = Digest::SHA256.hexdigest(PAYLOAD)

  def setup
    @index_root = Dir.mktmpdir
    @cache_root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = @index_root
    ReleaseCache.root = @cache_root
    ReleaseIndex.reset!
    @fetches = []
    write_index(
      'openipc.ts3516ev300-nor-lite.tgz' => {
        'size' => PAYLOAD.bytesize, 'digest' => "sha256:#{SHA}",
        'updated_at' => '2026-08-01T00:00:00Z', 'release' => 'nightly'
      },
      'u-boot-ts3516ev300-nor.bin' => {
        'size' => PAYLOAD.bytesize, 'digest' => nil,
        'updated_at' => '2024-10-27T00:00:00Z', 'release' => 'latest'
      }
    )
    serve(PAYLOAD)
  end

  def teardown
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseCache.root = nil
    ReleaseCache.downloader = nil
    ReleaseCache.lock_timeout = 60
    ReleaseIndex.reset!
    FileUtils.remove_entry(@index_root)
    FileUtils.remove_entry(@cache_root)
  end

  def write_index(assets)
    File.write(File.join(@index_root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'assets' => assets))
    ReleaseIndex.reset!
  end

  # Stands in for the network. Records every call so a test can prove a fetch
  # did or did not happen.
  def serve(body)
    ReleaseCache.downloader = lambda { |url, dest|
      @fetches << url
      IO.binwrite(dest, body)
      Digest::SHA256.hexdigest(body)
    }
  end

  # --- naming ---

  test 'an asset with a digest is stored under that digest' do
    path = ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    assert_equal SHA, File.basename(path)
    assert_equal PAYLOAD, IO.binread(path)
  end

  test 'an asset without a digest is stored under its size and name' do
    path = ReleaseCache.path('u-boot-ts3516ev300-nor.bin')
    assert_equal "size-#{PAYLOAD.bytesize}-u-boot-ts3516ev300-nor.bin", File.basename(path)
  end

  # --- fetching once ---

  test 'a second request for the same asset does not fetch again' do
    ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    assert_equal 1, @fetches.size
  end

  test 'a rebuilt asset gets a new name and is fetched again' do
    ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    rebuilt = "#{PAYLOAD}-rebuilt".b
    write_index('openipc.ts3516ev300-nor-lite.tgz' => {
                  'size' => rebuilt.bytesize, 'digest' => "sha256:#{Digest::SHA256.hexdigest(rebuilt)}",
                  'updated_at' => '2026-08-24T00:00:00Z', 'release' => 'nightly'
                })
    serve(rebuilt)

    path = ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    assert_equal rebuilt, IO.binread(path)
    assert_equal Digest::SHA256.hexdigest(rebuilt), File.basename(path)
  end

  # --- the index is the allowlist ---

  test 'a name upstream is not publishing is refused' do
    assert_raises(ReleaseCache::UnknownAsset) { ReleaseCache.path('openipc.not-a-real-soc.tgz') }
    assert_empty @fetches, 'it must not have gone looking'
  end

  test 'a crafted name cannot escape the cache directory' do
    ['../../etc/passwd', '/etc/passwd', 'openipc.x/../../y.tgz'].each do |name|
      assert_raises(ReleaseCache::UnknownAsset) { ReleaseCache.path(name) }
    end
    assert_empty @fetches
  end

  test 'a missing index is unavailable rather than unknown' do
    FileUtils.rm_f(File.join(@index_root, '.index.json'))
    ReleaseIndex.reset!
    assert_raises(ReleaseCache::Unavailable) { ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz') }
  end

  # --- verification ---

  test 'a body that does not match the digest is refused and not kept' do
    serve('something else entirely'.b * 10)
    error = assert_raises(ReleaseCache::Unavailable) { ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz') }
    assert_match(/bytes|sha256/, error.message)
    assert_empty Dir.glob(File.join(@cache_root, 'blobs', '*'))
  end

  test 'a body of the wrong length is refused even when nothing can be hashed against it' do
    ReleaseCache.downloader = ->(_url, dest) { IO.binwrite(dest, 'short'); Digest::SHA256.hexdigest('short') }
    assert_raises(ReleaseCache::Unavailable) { ReleaseCache.path('u-boot-ts3516ev300-nor.bin') }
    assert_empty Dir.glob(File.join(@cache_root, 'blobs', '*'))
  end

  test 'a failed fetch leaves no temporary file behind' do
    ReleaseCache.downloader = ->(_url, _dest) { raise ReleaseCache::Unavailable, 'upstream is down' }
    assert_raises(ReleaseCache::Unavailable) { ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz') }
    assert_empty Dir.glob(File.join(@cache_root, 'blobs', '.tmp-*'))
  end

  # --- the URL is built, not followed ---

  test 'the download url is built from the tag and the name' do
    ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    assert_equal ['https://github.com/OpenIPC/firmware/releases/download/nightly/openipc.ts3516ev300-nor-lite.tgz'],
                 @fetches
  end

  test 'the tag from the index decides which release is asked for' do
    ReleaseCache.path('u-boot-ts3516ev300-nor.bin')
    assert_equal ['https://github.com/OpenIPC/firmware/releases/download/latest/u-boot-ts3516ev300-nor.bin'],
                 @fetches
  end

  # --- what the blob looks like afterwards ---

  test 'the blob is readable and carries the upstream timestamp' do
    path = ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz')
    assert_equal '644', format('%o', File.stat(path).mode & 0o777)
    assert_equal Time.parse('2026-08-01T00:00:00Z'), File.mtime(path).utc
  end

  # --- concurrency ---

  test 'concurrent requests for one asset fetch it once' do
    slow = Mutex.new
    ReleaseCache.downloader = lambda { |url, dest|
      slow.synchronize { @fetches << url }
      sleep 0.05
      IO.binwrite(dest, PAYLOAD)
      SHA
    }
    4.times.map { Thread.new { ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz') } }.each(&:join)

    assert_equal 1, @fetches.size, 'the asset was fetched more than once'
    assert_equal PAYLOAD, IO.binread(File.join(@cache_root, 'blobs', SHA))
  end

  test 'waiting on a stuck fetch gives up rather than holding the thread' do
    FileUtils.mkdir_p(File.join(@cache_root, 'locks'))
    holder = File.open(File.join(@cache_root, 'locks', "#{SHA}.lock"), File::CREAT | File::RDWR, 0o644)
    holder.flock(File::LOCK_EX)
    ReleaseCache.lock_timeout = 0.3
    begin
      assert_raises(ReleaseCache::Unavailable) { ReleaseCache.path('openipc.ts3516ev300-nor-lite.tgz') }
    ensure
      holder.flock(File::LOCK_UN)
      holder.close
    end
  end
end
