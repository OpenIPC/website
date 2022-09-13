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
    "openipc-#{firmware}-#{soc}-#{sensor}-#{created_at.to_i}-#{file.filename}"
  end

  def image_dimensions
    [file.metadata['width'], file.metadata['height']].join('x')
  end

  def generate_timelaps
    in_dir="/tmp/#{mac_address}"
    FileUtils.mkdir_p in_dir

    command=[]
    command << "melt 0.jpg out=5"
    Snapshot.where(mac_address: mac_address).each_with_index do |s, idx|
      s.file.open do |f|
        in_file="#{idx}.jpg"
        tgt = File.join(in_dir, in_file)
        FileUtils.cp f, tgt unless File.exist?(tgt)
        command << "#{in_file} out=5 -mix 3 -mixer luma"
      end
    end
    command << "-consumer avformat:out.mp4"
    command << "width=1920 height=1080 frame_rate_num=30 sample_aspect_num=1 sample_aspect_den=1"
    command << "-video-track -quiet"
    command = command.join(" ")

    Dir.chdir in_dir do
      %x[#{command}]
    end

    # ffmpeg -framerate 30 -pattern_type glob -i "#{in_dir}/*.jpg" -s:v 1440x1080 -c:v libx264 -crf 17 -pix_fmt yuv420p -y my-timelapse.mp4
    # %x[ffmpeg -framerate 24 -pattern_type glob -i "#{in_dir}/*.jpg" -s hd1080 -c:v libx264 -crf 18 -preset ultrafast -vf "format=yuv420p" -tag:v hvc1 -y "#{in_dir}/265-tagged-hd.mp4" >&2]
    # %x[ffmpeg -pattern_type glob -i "#{in_dir}/*.jpg" -s:v 1280x720 -preset veryslow -c:v libx265 -crf 18 -pix_fmt yuv420p -tag:v hvc1 -y "#{in_dir}/265-tagged-hd.mp4" >&2]
  end


  private
    def time_interval
      return if ip_address.in?(Rails.application.credentials.ip.whitelisted)

      s = Snapshot.select(:created_at).where(mac_address: mac_address).order(:created_at).last
      if s && s.created_at > INTERVAL_LIMIT.ago + 2.minutes # hysteresis
        errors.add :base, "Please keep interval between photos at 15 minutes or more."
        raise TooSoon
      end
    end
end
