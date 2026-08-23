# frozen_string_literal: true

require 'test_helper'
require 'rubygems/package'
require 'zlib'
require 'tmpdir'

class FirmwareTest < ActiveSupport::TestCase
  StubVendor = Struct.new(:name)

  class StubSoc
    attr_reader :model_downcase, :vendor, :uboot_file

    def initialize(model:, vendor:, uboot_file:, linux_file:)
      @model_downcase = model
      @vendor = StubVendor.new(vendor)
      @uboot_file = uboot_file
      @linux_file = linux_file
    end

    def linux_file(_release, _flash_type)
      @linux_file
    end
  end

  UBOOT = "\x17\x04\x00\xEA".b + ("\x5A".b * 0x2000)
  KERNEL = "\x27\x05\x19\x56".b + ("\xA5".b * 0x1000)
  SQUASHFS = 'hsqs'.b + ("\xC3".b * 0x2000)
  UBI = 'UBI#'.b + ("\xD7".b * 0x3000)

  def setup
    @dir = Dir.mktmpdir
    @generated = []
  end

  def teardown
    FileUtils.remove_entry(@dir)
    @generated.each { |f| FileUtils.rm_f(f) }
  end

  def write_tgz(name, members)
    path = File.join(@dir, name)
    Zlib::GzipWriter.open(path) do |gz|
      Gem::Package::TarWriter.new(gz) do |tar|
        members.each do |member, data|
          tar.add_file_simple(member, 0o644, data.bytesize) { |io| io.write(data) }
        end
      end
    end
    path
  end

  def uboot_path
    @uboot_path ||= File.join(@dir, 'u-boot.bin').tap { |p| IO.binwrite(p, UBOOT) }
  end

  def build(model:, vendor:, members:, flash_type:, size:, release: 'ultimate')
    soc = StubSoc.new(model: model, vendor: vendor, uboot_file: uboot_path,
                      linux_file: write_tgz("openipc.#{model}-#{flash_type}-#{release}.tgz", members))
    fw = Firmware.new(size: size, flash_type: flash_type, release: release, soc: soc)
    @generated << fw.filepath
    FileUtils.rm_f(fw.filepath)
    fw
  end

  # --- filename ---

  test 'filename carries the flash type so nor and nand cannot share a cache file' do
    nor = Firmware.filename_for(soc_model: 'hi3516ev300', flash_type: 'nor', release: 'ultimate', size: 16)
    nand = Firmware.filename_for(soc_model: 'hi3516ev300', flash_type: 'nand', release: 'ultimate', size: 16)
    assert_equal 'openipc-hi3516ev300-nor-ultimate-16mb.bin', nor
    assert_equal 'openipc-hi3516ev300-nand-ultimate.bin', nand
    assert_not_equal nor, nand
  end

  # --- NOR regression ---

  test 'nor image keeps its layout' do
    fw = build(model: 'hi3518ev200', vendor: 'HiSilicon', flash_type: 'nor', size: 16,
               members: { 'uImage.hi3518ev200' => KERNEL, 'rootfs.squashfs.hi3518ev200' => SQUASHFS })
    fw.generate
    image = IO.binread(fw.filepath)

    assert_equal 16.megabytes, image.bytesize
    assert_equal UBOOT, image[0, UBOOT.bytesize]
    assert_equal KERNEL, image[0x50000, KERNEL.bytesize]
    assert_equal SQUASHFS, image[0x350000, SQUASHFS.bytesize]
  end

  # --- NAND ---

  test 'nand image takes rootfs.ubi and puts it at the ubi offset' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })
    fw.generate
    image = IO.binread(fw.filepath)

    assert_equal UBOOT, image[0, UBOOT.bytesize]
    assert_equal KERNEL, image[0x100000, KERNEL.bytesize], 'kernel belongs at the NAND offset'
    assert_equal UBI, image[0x400000, UBI.bytesize], 'rootfs.ubi belongs at the UBI offset'
    assert_equal 'UBI#', image[0x400000, 4]
  end

  test 'nand image stops after the payload and is page aligned' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })
    fw.generate
    size = File.size(fw.filepath)

    assert_equal 0, size % 2048, 'nand write rejects a non-page-aligned length'
    assert size >= 0x400000 + UBI.bytesize
    assert size < 0x8000000, 'a chip-sized image could not be staged in 64MB of RAM'
  end

  # --- the silent hole this PR closes ---

  test 'a nand tarball with no kernel is refused rather than served' do
    # SigmaStar and Rockchip carry the kernel as a volume inside rootfs.ubi, so
    # their NAND tarball has no uImage member and no full image can be built.
    fw = build(model: 'ssc338q', vendor: 'SigmaStar', flash_type: 'nand', size: 128,
               members: { 'rootfs.ubi.ssc338q' => UBI })

    error = assert_raises(Firmware::MissingMember) { fw.generate }
    assert_match(/uImage\.ssc338q/, error.message)
    assert_not File.exist?(fw.filepath), 'nothing may be left behind for send_file to serve'
  end

  test 'a nor tarball without a squashfs is refused rather than silently omitted' do
    # The original bug: NAND tarballs ship rootfs.ubi, the generator asked for
    # rootfs.squashfs, TarReader#seek returned nil, and the rootfs was dropped.
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nor', size: 16,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })

    error = assert_raises(Firmware::MissingMember) { fw.generate }
    assert_match(/rootfs\.squashfs\.hi3516ev300/, error.message)
  end

  test 'member order in the tarball does not matter' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'rootfs.ubi.hi3516ev300' => UBI, 'uImage.hi3516ev300' => KERNEL })
    fw.generate

    assert_equal KERNEL, IO.binread(fw.filepath)[0x100000, KERNEL.bytesize]
  end
end
