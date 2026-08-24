# frozen_string_literal: true

require 'test_helper'

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
end
