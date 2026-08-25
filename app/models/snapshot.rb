# frozen_string_literal: true

class Snapshot < ApplicationRecord

  class BlacklistedMac < StandardError
    #
  end

  class TooSoon < StandardError
    #
  end

  INTERVAL_LIMIT = 15.minutes

  # The newest snapshot from each camera seen in the last 24 hours, newest
  # first. Lived inline in SnapshotsController#index; the homepage mosaic wants
  # the same list, and two copies of a correlated subquery is one too many.
  #
  # The LEFT JOIN ... WHERE s2.id IS NULL is a greatest-n-per-group: a row
  # survives only when no newer row exists for its MAC.
  #
  # limit is interpolated after to_i, not bound, because it lands in a LIMIT
  # clause where a bind parameter is not accepted; to_i is what makes that safe.
  def self.latest_per_camera(limit: nil)
    sql = 'SELECT s1.* FROM snapshots s1 LEFT JOIN snapshots s2' \
          ' ON (s1.mac_address = s2.mac_address AND s1.created_at < s2.created_at)' \
          ' WHERE s2.id IS NULL AND s1.created_at > SUBDATE(NOW(), INTERVAL 1 DAY)' \
          ' ORDER BY created_at DESC'
    sql += " LIMIT #{limit.to_i}" if limit
    find_by_sql(sql)
  end

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

  # after_create fires inside the transaction, and perform_later on the :async
  # adapter hands the job to a thread pool that can pick it up before the
  # commit lands -- GlobalID then cannot find the row and ActiveJob discards
  # the job with a DeserializationError. It happened in bursts on the
  # quarter-hour, matching the cameras' cron upload cadence, and cost those
  # snapshots their pre-built variants: the wall fell back to generating them
  # on the first page view instead.
  after_create_commit :process_images

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

  # dig, not credentials.mac.blacklisted. Without config/master.key -- which is
  # what a fresh checkout and the test environment have -- credentials.mac is
  # nil, and the reader raised NoMethodError on every upload rather than simply
  # having nothing to blacklist. Production has the key, so this never showed
  # there; it made the API impossible to exercise anywhere else.
  def blacklisted_mac
    return unless mac_address.in?(Rails.application.credentials.dig(:mac, :blacklisted) || [])
    errors.add :base, 'This IP address is blacklisted.'
    raise BlacklistedMac
  end

  def time_interval
    return if ip_address.in?(Rails.application.credentials.dig(:ip, :whitelisted) || [])

    s = Snapshot.select(:created_at).where(mac_address: mac_address).order(:created_at).last
    if s && s.created_at > INTERVAL_LIMIT.ago + 2.minutes # hysteresis
      errors.add :base, 'Please keep interval between photos at 15 minutes or more.'
      raise TooSoon
    end
  end
end
