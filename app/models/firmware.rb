class Firmware
  def initialize(size: 8, soc: nil, release: 'lite')
    super()
    @soc = soc
    @size = size.to_i
    @release = release
  end

  def filename
    @filename ||= "openipc-#{@soc.model_downcase}-#{@release}-#{@size}mb.bin"
  end

  def filepath
    @filepath ||= File.join(Rails.root, 'public', 'files', filename)
  end

  def generate
    soc_model = @soc.model_downcase
    uboot_file = @soc.uboot_file
    unless File.exist?(uboot_file)
      puts "File #{uboot_file} not found."
      return
    end

    linux_file = @soc.linux_file(@release)
    unless File.exist?(linux_file)
      puts "File #{linux_file} not found."
      return
    end

    # file exists and it is newer than any of its parts
    if File.exist?(filepath) && File.mtime(uboot_file) < File.mtime(filepath) && File.mtime(linux_file) < File.mtime(filepath)
      puts "File #{filepath} exists and is fresh."
      return
    end

    firmware_size = @size.megabytes
    kernel_offset = 0x50000
    case @size
    when 8
      rootfs_offset = 0x250000
    when 16
      rootfs_offset = 0x350000
    when 32
      rootfs_offset = 0x350000
    else
      raise StandardError, 'Unknown flash size'
    end

    # create directory
    FileUtils.mkdir_p File.dirname(filepath)

    tmp_file = Tempfile.create
    IO.binwrite tmp_file, ("\xFF" * firmware_size)
    IO.binwrite tmp_file, IO.binread(uboot_file), 0
    Gem::Package::TarReader.new(Zlib::GzipReader.open(linux_file)) do |tar|
      kernel_file = "uImage.#{soc_model}"
      rootfs_file = "rootfs.squashfs.#{soc_model}"

      # workaround for Ingenic T31 where there's the same file for all models
      if @soc.family.eql?('T31')
        kernel_file = "uImage.t31"
        rootfs_file = "rootfs.squashfs.t31"
      end

      tar.seek(kernel_file) { |f| IO.binwrite tmp_file, f.read, kernel_offset }
      tar.seek(rootfs_file) { |f| IO.binwrite tmp_file, f.read, rootfs_offset }
    end
    FileUtils.mv tmp_file, filepath
  end
end
