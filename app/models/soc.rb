require "rubygems/package"

class Soc < ApplicationRecord
  COMMAND_BLOCKS = %w[backup bootloader firmware restore].freeze

  belongs_to :vendor
  has_many :custom_commands
  accepts_nested_attributes_for :custom_commands

  before_validation :generate_urlname
  validates :model, presence: true, uniqueness: { scope: :vendor_id }
  validates :urlname, presence: true, uniqueness: true

  RELEASES_ROOT = "/srv/github-releases"
  GH_DL_ROOT = "https://github.com/OpenIPC/firmware/releases/download/latest/%s"

  STATUS = {
    "neq": "No equipment on hands",
    "rnd": "Research and development",
    "hlp": "Looking for help",
    "wip": "Work in progress",
    "mvp": "Minimum viable product",
    "done": "Done and done!",
  }.freeze

  def self.find(id)
    find_by_urlname(id) || find_by_id(id)
  end

  def model_downcase
    @model_downcase ||= model.downcase
  end

  def to_param
    urlname
  end

  def bl_url
    format GH_DL_ROOT, uboot_filename
  end

  def fw_url(version)
    filename = linux_filename.dup
    filename.gsub("-br.tgz", "-#{version}-br.tgz") unless version.eql?("lite")
    format GH_DL_ROOT, filename
  end

  def tc_url
    format GH_DL_ROOT, toolchain_filename
  end

  def full_name
    [vendor.name, model].join(" ")
  end

  def firmware_filename
    @firmware_filename ||= "openipc.#{model_downcase}.8mb.bin"
  end

  def firmware_file
    @firmware_file ||= File.join(Rails.root, 'public', 'files', firmware_filename)
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end

  def kernel_file
    @kernel_file ||= "uImage.#{model_downcase}"
  end

  def linux_file
    @rootfs_file ||= File.join(RELEASES_ROOT, linux_filename)
  end

  def rootfs_file
    @kernel_file ||= "rootfs.squashfs.#{model_downcase}"
  end

  def uboot_file
    @uboot_file ||= File.join(RELEASES_ROOT, uboot_filename)
  end

  def full_firmware_path
    @full_firmware_path ||= "/tmp/openipc.#{model_downcase}.8mb.bin"
  end

  def generate_full_firmware
    unless File.exist?(uboot_file)
      puts "File #{uboot_file} not found"
      return
    end

    unless File.exist?(linux_file)
      puts "File #{linux_file} not found"
      return
    end

    if !File.exist?(firmware_file) || File.mtime(uboot_file) > File.mtime(firmware_file) || File.mtime(linux_file) > File.mtime(firmware_file)
      FileUtils.mkdir_p File.dirname(firmware_file)
      IO.binwrite firmware_file, ("\xFF" * 0x800000)
      IO.binwrite firmware_file, IO.binread(uboot_file), 0
      Gem::Package::TarReader.new(Zlib::GzipReader.open(linux_file)) do |tar|
        tar.seek("uImage.#{model_downcase}") { |f| IO.binwrite firmware_file, f.read, 0x50000 }
        tar.seek("rootfs.squashfs.#{model_downcase}") { |f| IO.binwrite firmware_file, f.read, 0x250000 }
      end
    end
  end

  private

    def generate_urlname
      self.urlname = model.downcase.gsub(' ', '-')
    end
end
