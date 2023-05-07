require "rubygems/package"

class Soc < ApplicationRecord
  belongs_to :vendor

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
    "done": "Done and done!"
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

  def full_name
    [vendor.name, model].join(" ")
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end

  def kernel_file
    @kernel_file ||= "uImage.#{model_downcase}"
  end

  def linux_file(release, flash_type)
    soc_name = model.downcase
    soc_name = 't31' if soc_name.start_with?('t31')
    linux_filename = "openipc.#{soc_name}-#{flash_type}-#{release}.tgz"

    @linux_file ||= File.join(RELEASES_ROOT, linux_filename)
  end

  def rootfs_file
    @rootfs_file ||= "rootfs.squashfs.#{model_downcase}"
  end

  def uboot_file
    @uboot_file ||= File.join(RELEASES_ROOT, uboot_filename)
  end

  def full_firmware_path
    @full_firmware_path ||= "/tmp/openipc.#{model_downcase}.8mb.bin"
  end

  private

  def generate_urlname
    self.urlname = model.downcase.gsub(' ', '-')
  end
end
