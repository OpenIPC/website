# frozen_string_literal: true

require 'test_helper'

# The half of #147 that nginx cannot do.
#
# deploy/nginx/conf.d/openipc-firmware-rate.conf bounds requests, which is a
# different quantity: a 16-32MB image is legitimately fetched as thirty range
# requests in a minute, and nginx cannot see whether the image being asked for
# is already on disk. So the limit that counts assemblies lives here, and these
# are the two behaviours that matter -- a cache miss over the limit is refused,
# and a cache hit never is.
class FirmwareRateLimitTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300', status: 'done',
                       uboot_filename: '', linux_filename: '')
    @cache = Dir.mktmpdir
    Firmware.cache_dir = @cache
  end

  teardown do
    Firmware.cache_dir = nil
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    ReleaseCache.root = nil
    ReleaseCache.downloader = nil
    FileUtils.remove_entry(@cache) if @cache
    FileUtils.remove_entry(@releases) if @releases
    FileUtils.remove_entry(@index_root) if @index_root
  end

  def download(size: 8)
    get "/cameras/vendors/#{@vendor.to_param}/socs/#{@soc.to_param}/download_full_image",
        params: { flash_size: size, flash_type: 'nor', fw_release: 'lite' }
  end

  test 'a build past the limit is refused with 429 and a Retry-After' do
    FirmwareBuild::LIMIT.times { FirmwareBuild.record('127.0.0.1') }

    download

    assert_response :too_many_requests
    assert_equal FirmwareBuild::WINDOW.to_i.to_s, response.headers['Retry-After']
  end

  # Under the limit the request carries on into the normal path. This SoC
  # publishes nothing, so it ends in the does-not-exist redirect -- what is
  # being asserted is that it got past the limiter, not what it found after.
  test 'a build under the limit is not refused' do
    (FirmwareBuild::LIMIT - 1).times { FirmwareBuild.record('127.0.0.1') }

    download

    assert_response :redirect
  end

  # The expensive thing is assembly. An image already on disk costs a file
  # read, and refusing one would punish exactly the visitor whose download was
  # interrupted and who is retrying -- 389 of the 1,695 requests in the
  # fortnight this was measured over were resumptions.
  test 'a cached image is sent even when the address is over the limit' do
    publish_a_soc_with_a_cached_image(size: 8)
    FirmwareBuild::LIMIT.times { FirmwareBuild.record('127.0.0.1') }

    download

    assert_response :success
    assert_equal 8.megabytes, response.body.bytesize
  end

  test 'sending a cached image does not count as a build' do
    publish_a_soc_with_a_cached_image(size: 8)

    assert_no_difference -> { FirmwareBuild.count } do
      download
    end
    assert_response :success
  end

  private

  # A SoC whose two release assets resolve, plus an image already in the cache
  # that is newer than both -- which is what Firmware#fresh? asks, and the only
  # way to reach the send-without-assembling path through a real request.
  #
  # The cached file has to satisfy #usable? as well: it exists, the web server
  # can read it, and it is exactly the flash size.
  def publish_a_soc_with_a_cached_image(size:)
    publish_the_release_assets
    cache_a_finished_image(size: size)
  end

  # Both assets resolve and are written to disk, so Firmware#build_if_needed
  # gets as far as asking whether the cached image is fresher than they are.
  def publish_the_release_assets
    @soc.update!(uboot_filename: 'u-boot-ts3516ev300-universal.bin')
    stub_the_release_index
    @soc.uboot_file
    @soc.linux_file('lite', 'nor')
  end

  def stub_the_release_index
    @index_root = Dir.mktmpdir
    @releases = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = @index_root
    File.write(File.join(@index_root, '.index.json'), JSON.generate(index_listing_both_assets))
    ReleaseIndex.reset!
    ReleaseCache.root = @releases
    ReleaseCache.downloader = ->(_url, dest) { IO.binwrite(dest, 'boot') && Digest::SHA256.hexdigest('boot') }
  end

  def index_listing_both_assets
    { 'generated_at' => '2026-08-24T00:00:00Z', 'aliases' => {},
      'assets' => { 'u-boot-ts3516ev300-universal.bin' => { 'size' => 4 },
                    'openipc.ts3516ev300-nor-lite.tgz' => { 'size' => 4 } } }
  end

  # What Firmware#usable? and #fresh? look for together: the file exists, the
  # web server can read it, it is exactly the flash size, and it is newer than
  # both release assets -- which is the send-without-assembling path.
  def cache_a_finished_image(size:)
    fw = Firmware.new(size: size, flash_type: 'nor', release: 'lite', soc: @soc)
    File.open(fw.filepath, 'wb') { |f| f.truncate(size.megabytes) }
    File.chmod(0o644, fw.filepath)
    File.utime(Time.now + 5, Time.now + 5, fw.filepath)
  end
end
