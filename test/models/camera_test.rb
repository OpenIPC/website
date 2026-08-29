# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

class CameraTest < ActiveSupport::TestCase
  def camera(flash_type:, firmware_version: 'ultimate')
    Camera.new(flash_type: flash_type, firmware_version: firmware_version)
  end

  # --- NOR regressions: these values render on every existing SoC page ---

  test 'nor geometry is unchanged' do
    c = camera(flash_type: 'nor16m')
    assert_equal '0x1000000', c.flash_size_hex
    assert_equal 16, c.flash_size
    assert_equal '0x50000', c.kernel_offset
    assert_equal '0x350000', c.rootfs_offset
    assert_equal '0xA00000', c.rootfs_max_size
    assert_equal '0x300000', c.kernel_max_size
  end

  test 'nor lite geometry is unchanged' do
    c = camera(flash_type: 'nor8m', firmware_version: 'lite')
    assert_equal '0x800000', c.flash_size_hex
    assert_equal '0x250000', c.rootfs_offset
    assert_equal '0x500000', c.rootfs_max_size
    assert_equal '0x200000', c.kernel_max_size
  end

  test 'staging size equals flash size on nor' do
    %w[nor8m nor16m nor32m].each do |t|
      c = camera(flash_type: t)
      assert_equal c.flash_size_hex, c.staging_size_hex, "staging size drifted for #{t}"
    end
  end

  # --- NAND ---

  test 'nand reports the real chip size rather than the 8MB nor default' do
    c = camera(flash_type: 'nand')
    assert_equal '0x8000000', c.flash_size_hex
    assert_equal 128, c.flash_size
    assert_equal '262144', c.flash_size_sectors
    assert_equal '0x40000', c.flash_size_blocks
  end

  test 'nand stages a RAM-sized buffer, not the whole chip' do
    c = camera(flash_type: 'nand')
    # A 128MiB mw.b would run off the end of RAM on a 64MB camera.
    assert_equal '0x1800000', c.staging_size_hex
    assert c.staging_size_hex.to_i(16) < c.flash_size_hex.to_i(16)
    # Must still cover the largest image the firmware Makefile can produce:
    # rootfs.ubi offset plus its 16384KB cap.
    assert c.staging_size_hex.to_i(16) >= c.rootfs_offset.to_i(16) + (16_384 * 1024)
  end

  test 'nand uses the ubi layout offsets, not the nor ones' do
    c = camera(flash_type: 'nand')
    assert_equal '0x100000', c.kernel_offset
    assert_equal '0x300000', c.kernel_max_size
    assert_equal '0x400000', c.rootfs_offset
    assert_equal '0x7c00000', c.rootfs_max_size
  end

  test 'nand rootfs partition runs to the end of the chip' do
    c = camera(flash_type: 'nand')
    assert_equal c.flash_size_hex.to_i(16),
                 c.rootfs_offset.to_i(16) + c.rootfs_max_size.to_i(16)
  end

  test 'nand? only answers for nand' do
    assert camera(flash_type: 'nand').nand?
    assert_not camera(flash_type: 'nor16m').nand?
  end

  # --- the malformed-erase guard ---

  test 'overlay_max_size refuses to return a negative length' do
    # This is what rendered `nand erase 0xD50000 0x-550000`: an overlay offset
    # past the end of the flash. U-Boot parses that size as 0 and erases nothing.
    #
    # No combination of flash type and edition produces that any more -- the
    # layout is chosen by chip size, so the overlay always lands inside the
    # chip -- so the guard is exercised directly rather than through a pairing
    # that used to be incoherent.
    c = camera(flash_type: 'nor8m', firmware_version: 'lite')
    c.stub(:overlay_offset, '0xD50000') do
      assert c.overlay_offset.to_i(16) > c.flash_size_hex.to_i(16)
      assert_raises(StandardError) { c.overlay_max_size }
    end
  end

  test 'overlay_max_size is positive where the layout is coherent' do
    c = camera(flash_type: 'nor16m', firmware_version: 'ultimate')
    assert_equal '0x2b0000', c.overlay_max_size
  end

  # --- the layout follows the chip, not the edition ---

  test 'a 16MB chip gets the 16MB layout whatever edition is selected' do
    # The wizard forces Lite on 16MB, so this is what every 16MB NOR camera
    # gets. It used to be handed the 8MB layout, while `run urnor16m` wrote the
    # rootfs at 0x350000 and the page then erased from 0x750000 -- 733,184
    # bytes into what had just been written.
    %w[lite ultimate].each do |edition|
      c = camera(flash_type: 'nor16m', firmware_version: edition)
      assert_equal '0x350000', c.rootfs_offset, "16MB/#{edition} rootfs offset"
      assert_equal '0xA00000', c.rootfs_max_size, "16MB/#{edition} rootfs max"
      assert_equal '0x300000', c.kernel_max_size, "16MB/#{edition} kernel max"
      assert_equal '0xD50000', c.overlay_offset, "16MB/#{edition} overlay offset"
    end
  end

  test 'an 8MB chip gets the 8MB layout whatever edition is selected' do
    %w[lite ultimate].each do |edition|
      c = camera(flash_type: 'nor8m', firmware_version: edition)
      assert_equal '0x250000', c.rootfs_offset, "8MB/#{edition} rootfs offset"
      assert_equal '0x500000', c.rootfs_max_size, "8MB/#{edition} rootfs max"
      assert_equal '0x200000', c.kernel_max_size, "8MB/#{edition} kernel max"
      assert_equal '0x750000', c.overlay_offset, "8MB/#{edition} overlay offset"
    end
  end

  test 'a 32MB chip uses the 16MB layout, as the nor16m commands it is given do' do
    # Cameras::SocsController rewrites nor32m to the nor16m command set because
    # upstream defines no mtdpartsnor32m; the offsets have to follow.
    c = camera(flash_type: 'nor32m', firmware_version: 'ultimate')
    assert_equal '0x350000', c.rootfs_offset
    assert_equal '0xD50000', c.overlay_offset
  end

  # --- the chip and the layout are two questions ---

  test 'the layout defaults to the one that matches the chip' do
    assert_equal 'nor8m', camera(flash_type: 'nor8m').partition_layout
    assert_equal 'nor16m', camera(flash_type: 'nor16m').partition_layout
    assert_equal 'nor16m', camera(flash_type: 'nor32m').partition_layout
    assert_equal 'nand', camera(flash_type: 'nand').partition_layout
  end

  # The configuration the report came from: a 16MB part wearing the 8MB layout,
  # which is what a camera flashed from the 8MB image already has. The offsets
  # have to be the 8MB ones and the erase has to be the whole chip, because
  # rootfs_data is `-` in every mtdparts and so runs to the end of the device
  # whichever layout is written on it.
  test 'an 8MB layout on a 16MB chip keeps 8MB offsets and a 16MB erase' do
    c = camera(flash_type: 'nor16m', firmware_version: 'lite')
    c.partition_layout = 'nor8m'

    assert_equal 8, c.layout_size
    assert_equal '0x250000', c.rootfs_offset
    assert_equal '0x500000', c.rootfs_max_size
    assert_equal '0x200000', c.kernel_max_size
    assert_equal '0x750000', c.overlay_offset

    assert_equal 16, c.flash_size
    assert_equal '0x1000000', c.flash_size_hex
    # The whole of the rest of the chip: 0x1000000 - 0x750000, not the 0xb0000
    # an 8MB part would leave. That difference is the untouched half a full
    # reflash used to leave the old overlay sitting in.
    assert_equal '0x8b0000', c.overlay_max_size
  end

  test 'the 16MB layout is refused on an 8MB chip, which cannot hold it' do
    c = camera(flash_type: 'nor8m')
    c.partition_layout = 'nor16m'

    assert_equal 'nor8m', c.partition_layout
    assert_equal '0x250000', c.rootfs_offset
  end

  test 'an unrecognised layout is the same as none' do
    c = camera(flash_type: 'nor16m')
    c.partition_layout = 'nor64m'

    assert_equal 'nor16m', c.partition_layout
  end

  test 'nand has one layout and does not take a nor one' do
    c = camera(flash_type: 'nand')
    c.partition_layout = 'nor8m'

    assert_equal 'nand', c.partition_layout
    assert_equal '0x400000', c.rootfs_offset
  end

  test 'every nor layout leaves the overlay inside the chip' do
    %w[nor8m nor16m nor32m].each do |flash|
      %w[lite ultimate].each do |edition|
        c = camera(flash_type: flash, firmware_version: edition)
        assert c.overlay_offset.to_i(16) < c.flash_size_hex.to_i(16),
               "#{flash}/#{edition} puts the overlay past the end of the chip"
        assert_nothing_raised { c.overlay_max_size }
      end
    end
  end

  # --- editions that are not published ---

  test 'an edition upstream does not build is replaced, and says what was asked for' do
    vendor = Vendor.create!(name: 'Testco2')
    soc = Soc.create!(vendor: vendor, model: 'TS377D',
                      linux_filename: 'openipc.ts377d-nor-lite.tgz')
    camera = Camera.new(soc: soc, flash_type: 'nor16m', firmware_version: 'ultimate')

    with_index(['openipc.ts377d-nor-lite.tgz']) do
      assert_equal 'ultimate', camera.use_published_release!
      assert_equal 'lite', camera.firmware_version
      assert_equal 'Lite', camera.firmware_version_name
    end
  end

  test 'an edition that is published is left alone' do
    vendor = Vendor.create!(name: 'Testco3')
    soc = Soc.create!(vendor: vendor, model: 'TS338Q',
                      linux_filename: 'openipc.ts338q-nor-lite.tgz')
    camera = Camera.new(soc: soc, flash_type: 'nor32m', firmware_version: 'ultimate')

    with_index(['openipc.ts338q-nor-lite.tgz', 'openipc.ts338q-nor-ultimate.tgz']) do
      assert_nil camera.use_published_release!
      assert_equal 'ultimate', camera.firmware_version
    end
  end

  test 'the flash type decides which list is consulted' do
    vendor = Vendor.create!(name: 'Testco4')
    soc = Soc.create!(vendor: vendor, model: 'TS1109',
                      linux_filename: 'openipc.ts1109-nand-lite.tgz')

    with_index(['openipc.ts1109-nand-lite.tgz', 'openipc.ts1109-nor-ultimate.tgz']) do
      nand = Camera.new(soc: soc, flash_type: 'nand', firmware_version: 'ultimate')
      assert_equal 'ultimate', nand.use_published_release!
      assert_equal 'lite', nand.firmware_version

      nor = Camera.new(soc: soc, flash_type: 'nor32m', firmware_version: 'ultimate')
      assert_nil nor.use_published_release!
    end
  end

  test 'an edition with no translation is labelled by its own name' do
    camera = Camera.new(firmware_version: 'zephyr')

    assert_equal 'Zephyr', camera.firmware_version_name
  end

  # --- the permanent link ---

  # permalink emitted `var` while Cameras::SocsController#show has only ever
  # read `ver`, so the edition was the single field the link dropped: a link
  # for Ultimate on a 32MB chip reopened as Lite. Nothing else diverged, which
  # is why it went unnoticed -- so this pins the whole key set rather than the
  # one key, against the reader in `show`.
  test 'the permanent link is spelled with the keys show reads back' do
    camera = Camera.new(camera_mac_address: 'aa:bb:cc:dd:ee:ff', camera_ip_address: '10.0.0.5',
                        server_ip_address: '10.0.0.1', network_interface: 'wifi',
                        flash_type: 'nor32m', firmware_version: 'ultimate', sd_card_slot: 'sd')

    keys = Rack::Utils.parse_query(camera.permalink.delete_prefix('?')).keys

    assert_equal %w[mac cip sip net rom part ver sd].sort, keys.sort
  end

  test 'the permanent link carries a layout that is not the chip default' do
    camera = Camera.new(camera_mac_address: 'aa:bb:cc:dd:ee:ff', flash_type: 'nor16m',
                        firmware_version: 'lite', network_interface: 'eth', sd_card_slot: 'nosd')
    camera.partition_layout = 'nor8m'

    assert_includes camera.permalink, '&part=nor8m'
  end

  test 'the permanent link carries every value it was built from' do
    camera = Camera.new(camera_mac_address: 'aa:bb:cc:dd:ee:ff', camera_ip_address: '10.0.0.5',
                        server_ip_address: '10.0.0.1', network_interface: 'wifi',
                        flash_type: 'nor32m', firmware_version: 'ultimate', sd_card_slot: 'sd')

    query = Rack::Utils.parse_query(camera.permalink.delete_prefix('?'))

    # The MAC is the one field that changes shape: colons are not legal in a
    # query string unescaped, and `show` turns the dashes back.
    assert_equal({ 'mac' => 'aa-bb-cc-dd-ee-ff', 'cip' => '10.0.0.5', 'sip' => '10.0.0.1',
                   'net' => 'wifi', 'rom' => 'nor32m', 'part' => 'nor16m', 'ver' => 'ultimate',
                   'sd' => 'sd' }, query)
  end

  def with_index(assets)
    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'aliases' => {},
                             'assets' => assets.to_h { |n| [n, { 'size' => 1 }] }))
    ReleaseIndex.reset!
    yield
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end
end
