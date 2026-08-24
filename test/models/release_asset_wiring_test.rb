# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

# Soc#uboot_file and #linux_file are the only two places that name an upstream
# asset, and they are what the cutover flips.
class ReleaseAssetWiringTest < ActiveSupport::TestCase
  def setup
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300',
                       uboot_filename: 'u-boot-ts3516ev300-nor.bin',
                       linux_filename: 'openipc.ts3516ev300-nor-lite.tgz')
    @mirror = Dir.mktmpdir
  end

  def teardown
    ENV.delete('RELEASE_MIRROR_ROOT')
    FileUtils.remove_entry(@mirror)
  end

  test 'the mirror wins while it is configured and holds the file' do
    FileUtils.touch(File.join(@mirror, 'u-boot-ts3516ev300-nor.bin'))
    ENV['RELEASE_MIRROR_ROOT'] = @mirror

    assert_equal File.join(@mirror, 'u-boot-ts3516ev300-nor.bin'), @soc.uboot_file
  end

  test 'a file the mirror does not have falls through to the cache' do
    ENV['RELEASE_MIRROR_ROOT'] = @mirror # empty
    called = []
    ReleaseCache.stub(:path, ->(name) { called << name; "/cache/#{name}" }) do
      assert_equal '/cache/u-boot-ts3516ev300-nor.bin', @soc.uboot_file
    end
    assert_equal ['u-boot-ts3516ev300-nor.bin'], called
  end

  test 'with no mirror configured everything goes to the cache' do
    called = []
    ReleaseCache.stub(:path, ->(name) { called << name; "/cache/#{name}" }) do
      @soc.linux_file('lite', 'nor')
    end
    assert_equal ['openipc.ts3516ev300-nor-lite.tgz'], called
  end

  test 'an empty mirror root is treated as unset rather than as /' do
    ENV['RELEASE_MIRROR_ROOT'] = ''
    ReleaseCache.stub(:path, ->(name) { "/cache/#{name}" }) do
      assert_equal '/cache/u-boot-ts3516ev300-nor.bin', @soc.uboot_file
    end
  end

  test 'linux_file still answers per release and flash type through the cache' do
    asked = []
    ReleaseCache.stub(:path, ->(name) { asked << name; "/cache/#{name}" }) do
      @soc.linux_file('lite', 'nor')
      @soc.linux_file('ultimate', 'nor')
      @soc.linux_file('ultimate', 'nand')
    end
    assert_equal %w[openipc.ts3516ev300-nor-lite.tgz
                    openipc.ts3516ev300-nor-ultimate.tgz
                    openipc.ts3516ev300-nand-ultimate.tgz], asked
  end

  # --- names an admin could put in the database ---

  test 'a name that climbs out of the mirror root is refused' do
    # uboot_filename is an admin-editable column, and Firmware#assemble reads
    # whatever this returns as the bootloader before download_full_image sends
    # the assembled image. Without this, that is host file disclosure.
    ENV['RELEASE_MIRROR_ROOT'] = @mirror
    outside = File.join(@mirror, 'outside.txt')
    FileUtils.mkdir_p(File.dirname(outside))
    File.write(File.expand_path(File.join(@mirror, '..', 'secret.txt')), 'not yours')

    soc = Soc.create!(vendor: @vendor, model: 'TS3516CV500',
                      uboot_filename: '../secret.txt',
                      linux_filename: 'openipc.ts3516cv500-nor-lite.tgz')

    assert_raises(ReleaseCache::UnknownAsset) { soc.uboot_file }
  ensure
    FileUtils.rm_f(File.expand_path(File.join(@mirror, '..', 'secret.txt')))
  end

  test 'every shape of escape is refused, and none reaches the cache' do
    ENV['RELEASE_MIRROR_ROOT'] = @mirror
    reached = []
    ReleaseCache.stub(:path, ->(name) { reached << name; '/cache/x' }) do
      ['../../etc/passwd', 'sub/dir.bin', '/etc/passwd', '.hidden.bin', "bad\nname.bin", ''].each do |bad|
        soc = Soc.new(vendor: @vendor, model: 'X', uboot_filename: bad, linux_filename: 'openipc.x-nor-lite.tgz')
        assert_raises(ReleaseCache::UnknownAsset, "#{bad.inspect} was allowed") { soc.uboot_file }
      end
    end
    assert_empty reached, 'a refused name still reached the cache'
  end

  test 'an ordinary name is still allowed through' do
    ENV['RELEASE_MIRROR_ROOT'] = @mirror
    FileUtils.touch(File.join(@mirror, 'u-boot-ts3516ev300-nor.bin'))
    assert_equal File.join(@mirror, 'u-boot-ts3516ev300-nor.bin'), @soc.uboot_file
  end
end
