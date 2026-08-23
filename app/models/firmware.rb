# frozen_string_literal: true

require 'rubygems/package'

class Firmware
  # NAND full images stop after the UBI payload rather than spanning the chip.
  # The parts are 128MiB but the cameras have 64MB of RAM, so a chip-sized
  # image could never be staged at the load address to be written from.
  NAND_PAGE = 2048
  NAND_KERNEL_OFFSET = 0x100000
  NAND_ROOTFS_OFFSET = 0x400000

  NOR_KERNEL_OFFSET = 0x50000
  NOR_SIZES = [8, 16, 32].freeze

  class MissingMember < StandardError; end
  class InvalidFlashSize < StandardError; end

  def initialize(size: 8, flash_type: 'nor', release: 'lite', soc: nil)
    super()
    @soc = soc
    @size = size.to_i
    @flash_type = flash_type
    @release = release
  end

  # Built here rather than in the view so the TFTP command on the installation
  # page and the file the download actually produces cannot drift apart.
  def self.filename_for(soc_model:, flash_type:, release:, size:)
    return "openipc-#{soc_model}-nand-#{release}.bin" if flash_type.to_s.eql?('nand')

    "openipc-#{soc_model}-nor-#{release}-#{size}mb.bin"
  end

  def filename
    @filename ||= self.class.filename_for(soc_model: @soc.model_downcase, flash_type: @flash_type,
                                          release: @release, size: @size)
  end

  def filepath
    @filepath ||= File.join(Rails.root, 'public', 'files', filename)
  end

  def nand?
    @flash_type.to_s.eql?('nand')
  end

  def generate
    validate_size!

    uboot_file = @soc.uboot_file
    unless File.exist?(uboot_file)
      puts "File #{uboot_file} not found."
      return
    end

    linux_file = @soc.linux_file(@release, @flash_type)
    unless File.exist?(linux_file)
      puts "File #{linux_file} not found."
      return
    end

    # file exists and it is newer than any of its parts
    if File.exist?(filepath) &&
        File.mtime(uboot_file) < File.mtime(filepath) &&
        File.mtime(linux_file) < File.mtime(filepath)
      puts "File #{filepath} exists and is fresh."
      return
    end

    data, present = read_members(linux_file, [kernel_member, rootfs_member])
    kernel = fetch_member!(data, kernel_member, linux_file, present)
    rootfs = fetch_member!(data, rootfs_member, linux_file, present)

    # Resolve every offset and the length before allocating anything, so an
    # unsupported size cannot get as far as filling a buffer with it.
    k_offset = kernel_offset
    r_offset = rootfs_offset
    size = image_size(rootfs)

    # create directory
    FileUtils.mkdir_p File.dirname(filepath)

    tmp_file = Tempfile.create
    IO.binwrite tmp_file, ("\xFF" * size)
    IO.binwrite tmp_file, IO.binread(uboot_file), 0
    IO.binwrite tmp_file, kernel, k_offset
    IO.binwrite tmp_file, rootfs, r_offset
    FileUtils.mv tmp_file, filepath
  end

  private

  # size is a request parameter, so it has to be checked before it can be turned
  # into an allocation. Guarding here rather than relying on rootfs_offset to
  # raise later keeps a crafted ?flash_size=<huge> from filling a buffer first.
  # NAND ignores it entirely: that image is sized from its payload.
  def validate_size!
    return if nand?
    return if NOR_SIZES.include?(@size)

    raise InvalidFlashSize, "unsupported NOR flash size #{@size}MB (expected #{NOR_SIZES.join(', ')})"
  end

  # One pass, reading only the two bodies we need -- rootfs.ubi runs to 16MB and
  # buffering every member would hold far more than the image itself. Names are
  # collected for the error message, which costs nothing: skipped entries are
  # stepped over by header, not read.
  #
  # A pass rather than two TarReader#seek calls because seek scans forward from
  # the current position, so successive seeks only worked while the members
  # happened to be in the order asked for.
  def read_members(linux_file, wanted)
    data = {}
    present = []
    Gem::Package::TarReader.new(Zlib::GzipReader.open(linux_file)) do |tar|
      tar.each do |entry|
        next unless entry.file?

        present << entry.full_name
        data[entry.full_name] = entry.read if wanted.include?(entry.full_name)
      end
    end
    [data, present]
  end

  # The bug this class shipped for years: TarReader#seek yields nothing when the
  # member is absent and the return value was never checked, so a NAND build --
  # whose rootfs is named rootfs.ubi, not rootfs.squashfs -- produced an image
  # with no root filesystem in it at all, served as though it were complete.
  # Refusing loudly is also what keeps SigmaStar and Rockchip out: their NAND
  # tarballs carry no kernel member, because the kernel is a volume inside the
  # UBI image rather than a partition of its own.
  def fetch_member!(data, name, linux_file, present)
    body = data[name]
    return body if body

    listed = present.reject { |k| k.end_with?('.md5sum') }.join(', ')
    raise MissingMember, "#{name} is not in #{File.basename(linux_file)} (members: #{listed})"
  end

  def soc_model
    @soc_model ||= @soc.model_downcase
  end

  def kernel_member
    # Ingenic ships one kernel for the whole family rather than one per model.
    return 'uImage.t31' if soc_model.starts_with?('t31')
    return 'uImage.t40' if soc_model.starts_with?('t40')

    "uImage.#{soc_model}"
  end

  def rootfs_member
    return "rootfs.ubi.#{soc_model}" if nand?
    return 'rootfs.squashfs.t31' if soc_model.starts_with?('t31')
    return 'rootfs.squashfs.t40' if soc_model.starts_with?('t40')

    "rootfs.squashfs.#{soc_model}"
  end

  def kernel_offset
    nand? ? NAND_KERNEL_OFFSET : NOR_KERNEL_OFFSET
  end

  def rootfs_offset
    return NAND_ROOTFS_OFFSET if nand?
    return 0x250000 if @soc.vendor.name.eql?('SigmaStar') || @soc.vendor.name.eql?('Ingenic')

    case @size
    when 8
      0x250000
    when 16, 32
      0x350000
    else
      raise InvalidFlashSize, "unsupported NOR flash size #{@size}MB"
    end
  end

  def image_size(rootfs)
    return @size.megabytes unless nand?

    end_of_rootfs = NAND_ROOTFS_OFFSET + rootfs.bytesize
    # Page-align so the generated image can be written with ${filesize}; U-Boot
    # `nand write` rejects a length that is not a multiple of the page size.
    (end_of_rootfs + NAND_PAGE - 1) / NAND_PAGE * NAND_PAGE
  end
end
