# frozen_string_literal: true

require 'test_helper'

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
    c = camera(flash_type: 'nor8m', firmware_version: 'ultimate')
    assert c.overlay_offset.to_i(16) > c.flash_size_hex.to_i(16)
    assert_raises(StandardError) { c.overlay_max_size }
  end

  test 'overlay_max_size is positive where the layout is coherent' do
    c = camera(flash_type: 'nor16m', firmware_version: 'ultimate')
    assert_equal '0x2b0000', c.overlay_max_size
  end
end
