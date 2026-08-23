# frozen_string_literal: true

class Snapshot < ApplicationRecord

  class BlacklistedMac < StandardError
    #
  end

  class TooSoon < StandardError
    #
  end

  INTERVAL_LIMIT = 15.minutes

  # Uploads may be HEIF (HEVC/AVC) as well as JPEG. Render every variant as JPEG
  # so the wall displays in all browsers (HEIF is decodable only by Safari) and
  # stays small. Decoding HEIF sources requires the server's libvips to be built
  # with libheif support.
  # dependent stays at the default :purge_later. purge_file_now below does the
  # real work inline, and by the time purge_later runs there is nothing left to
  # do -- but keeping it means a second chance if that hook ever fails. The
  # alternative, dependent: false, makes Rails call detach instead, which drops
  # the attachment and leaves the blob behind: the exact bug being fixed.
  has_one_attached :file do |attachable|
    attachable.variant :icon,   resize_to_limit: [90, 60],     format: :jpeg, saver: { quality: 80, strip: true }
    attachable.variant :icon2,  resize_to_limit: [240, 135],   format: :jpeg, saver: { quality: 80, strip: true }
    attachable.variant :thumb,  resize_to_limit: [480, 360],   format: :jpeg, saver: { quality: 80, strip: true }
    attachable.variant :fullhd, resize_to_limit: [1920, 1080], format: :jpeg, saver: { quality: 85, strip: true }
  end

  # Purge the blob synchronously, before Rails' own after_destroy_commit hook
  # gets the chance to do it with purge_later.
  #
  # has_one_attached's default cleanup is purge_later, which only detaches the
  # attachment and enqueues ActiveStorage::PurgeJob for the blob. No durable
  # queue adapter is configured, so that job runs on :async -- an in-process
  # thread pool that is simply dropped on restart. The result is a detached
  # blob, its variant records and its file left behind with nothing referencing
  # them, which is how ~93,000 orphans accumulated. purge does the same work
  # inline and cannot be lost.
  before_destroy :purge_file_now, prepend: true

  validates :file, presence: true, blob: { content_type: :image, size_range: (10.kilobytes)..(5.megabytes) }
  validates :mac_address, presence: true, format: MAC_ADDRESS_FORMAT
  validate :blacklisted_mac
  validate :time_interval

  after_create :process_images

  def process_images
    ProcessImagesJob.perform_later(self)
  end

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
    in_dir = "/tmp/#{mac_address}"
    FileUtils.mkdir_p in_dir

    command = []
    command << 'melt 0.jpg out=5'
    Snapshot.where(mac_address: mac_address).each_with_index do |s, idx|
      s.file.open do |f|
        in_file = "#{idx}.jpg"
        tgt = File.join(in_dir, in_file)
        FileUtils.cp f, tgt unless File.exist?(tgt)
        command << "#{in_file} out=5 -mix 3 -mixer luma"
      end
    end
    command << '-consumer avformat:out.mp4'
    command << 'width=1920 height=1080 frame_rate_num=30 sample_aspect_num=1 sample_aspect_den=1'
    command << '-video-track -quiet'
    command = command.join(' ')

    Dir.chdir in_dir do
      %x[#{command}]
    end

    # ffmpeg -framerate 30 -pattern_type glob -i "#{in_dir}/*.jpg" -s:v 1440x1080 -c:v libx264 -crf 17 -pix_fmt yuv420p -y my-timelapse.mp4
    # %x[ffmpeg -framerate 24 -pattern_type glob -i "#{in_dir}/*.jpg" -s hd1080 -c:v libx264 -crf 18 -preset ultrafast -vf "format=yuv420p" -tag:v hvc1 -y "#{in_dir}/265-tagged-hd.mp4" >&2]
    # %x[ffmpeg -pattern_type glob -i "#{in_dir}/*.jpg" -s:v 1280x720 -preset veryslow -c:v libx265 -crf 18 -pix_fmt yuv420p -tag:v hvc1 -y "#{in_dir}/265-tagged-hd.mp4" >&2]
  end

  private

  def purge_file_now
    return unless file.attached?

    # Purge the variant images first, explicitly. Destroying the parent blob
    # cascades to its variant_records, but each of those is an
    # ActiveStorage::VariantRecord whose own has_one_attached :image defers to
    # purge_later -- framework-internal and not configurable from here. On the
    # :async adapter that deferred work is routinely lost, leaving one orphan
    # blob and one file per materialised variant. Measured on production: a
    # snapshot with a single icon variant leaked a 756-byte blob on destroy.
    file.blob.variant_records.each do |variant_record|
      variant_record.image.purge if variant_record.image.attached?
    rescue ActiveStorage::FileNotFoundError
      variant_record.image.attachment&.purge
    end

    file.purge
  rescue ActiveStorage::FileNotFoundError
    # Row outlived its file; still drop the attachment and blob rows.
    file.attachment&.purge
  end

  def blacklisted_mac
    return unless mac_address.in?(Rails.application.credentials.mac.blacklisted)
    errors.add :base, 'This IP address is blacklisted.'
    raise BlacklistedMac
  end

  def time_interval
    return if ip_address.in?(Rails.application.credentials.ip.whitelisted)

    s = Snapshot.select(:created_at).where(mac_address: mac_address).order(:created_at).last
    if s && s.created_at > INTERVAL_LIMIT.ago + 2.minutes # hysteresis
      errors.add :base, 'Please keep interval between photos at 15 minutes or more.'
      raise TooSoon
    end
  end
end
