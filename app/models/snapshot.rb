class Snapshot < ApplicationRecord

  class TooSoon < StandardError
    #
  end

  INTERVAL_LIMIT = 15.minutes
  MAC_ADDRESS_BLACKLIST = %w[00:00:00:00:00:00 00:00:23:34:45:66]

  has_one_attached :file do |attachable|
    attachable.variant :icon, resize_to_limit: [90, 60]
    attachable.variant :icon2, resize_to_limit: [240, 135]
    attachable.variant :thumb, resize_to_limit: [480, 360]
    attachable.variant :fullhd, resize_to_limit: [1920, 1080]
  end

  validates :mac_address, presence: true, format: MAC_ADDRESS_FORMAT,
            exclusion: { in: MAC_ADDRESS_BLACKLIST, message: "is fake. Restore camera's original MAC address." }
  validates :file, presence: true, blob: { content_type: :image, size_range: (10.kilobytes)..(5.megabytes) }
  validate :time_interval

  def mac_address_dec
    mac_address.gsub(':', '').to_i(16)
  end

  def filename_for_download
    "openipc-#{firmware}-#{soc}-#{sensor}-#{created_at.to_i}.jpg"
  end

  def image_dimensions
    [file.metadata['width'], file.metadata['height']].join('x')
  end

  private
    def time_interval
      s = Snapshot.select(:created_at).where(mac_address: mac_address).order(:created_at).last
      if s && s.created_at > INTERVAL_LIMIT.ago + 2.minutes # hysteresis
        errors.add :base, "Please keep interval between photos at 15 minutes or more."
        raise TooSoon
      end
    end
end
