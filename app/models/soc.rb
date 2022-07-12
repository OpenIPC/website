class Soc < ApplicationRecord
  belongs_to :vendor

  validates :model, uniqueness: { scope: :vendor_id }

  GH_DL_ROOT="https://github.com/OpenIPC/firmware/releases/download/latest/%s"

  STATUS={
    "done": "All functions are supported",
    "hlp": "Looking for help",
    "neq": "No equipment on hands",
    "mvp": "Minimum viable product",
    "rnd": "Research and development",
    "wip": "Work in progress",
  }.freeze

  def self.find(id)
    find_by(['lower(model) = ?', id]) || find_by_id(id)
  end

  def to_param
    urlname
  end

  def urlname
    model.downcase
  end

  def bl_url
    format GH_DL_ROOT, uboot_filename
  end

  def fw_url
    format GH_DL_ROOT, linux_filename
  end

  def full_name
    [vendor.name, model].join(" ")
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end
end
