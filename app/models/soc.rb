class Soc < ApplicationRecord
  belongs_to :vendor

  before_validation :generate_urlname
  validates :model, uniqueness: { scope: :vendor_id }
  validates :urlname, uniqueness: true

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

  def to_param
    urlname
  end

  def bl_url
    format GH_DL_ROOT, uboot_filename
  end

  def fw_url
    format GH_DL_ROOT, linux_filename
  end

  def tc_url
    format GH_DL_ROOT, toolchain_filename
  end

  def full_name
    [vendor.name, model].join(" ")
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end

  private

    def generate_urlname
      self.urlname = model.downcase.gsub(' ', '-')
    end
end
