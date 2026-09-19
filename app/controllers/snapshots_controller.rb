# frozen_string_literal: true

class SnapshotsController < ApplicationController
  skip_before_action :verify_authenticity_token
  before_action :find_snapshot, only: [:oneday, :show, :download]
  before_action :find_camera, only: [:camera]

  def index
    page = params[:page] || 1
    @snapshots = Kaminari.paginate_array(Snapshot.latest_per_camera).page(page).per(18)
    @page_title = "Open Wall, page #{page}"
    render 'snapshots/index'
  end

  def create
    # Retention is handled by a nightly cron (openipc-purge-snapshots), not on
    # the upload path. Enqueuing it per-request meant a full table scan for
    # every camera POST.
    @snapshot = Snapshot.new
    @snapshot.ip_address = request.remote_ip
    if @snapshot.update(permitted_params)
      head :created, location: snapshot_path(@snapshot)
    else
      head :unsupported_media_type, 'X-Error': @snapshot.errors.full_messages.join('. ')
    end
  rescue Snapshot::BlacklistedMac
    head :forbidden
  rescue Snapshot::TooSoon
    offset = 0
    s = Snapshot.where(mac_address: permitted_params[:mac_address]).order(created_at: :desc).first
    offset = (Time.now.utc - s.created_at.utc).to_i unless s.nil?
    head :too_many_requests, retry_after: Snapshot::INTERVAL_LIMIT - offset
  end

  def show
    daily_snapshots_new_to_old
    @page_title = "Open Wall, image ##{params[:id]}"
    render 'snapshots/show'
  end

  def camera
    respond_to do |format|
      format.jpg { send_camera_jpeg }
      format.html do
        daily_snapshots_new_to_old
        @page_title = "Open Wall, image ##{params[:id]}"
        render 'snapshots/show'
      end
    end
  end

  def download
    send_blob(@snapshot.file, @snapshot.filename_for_download)
  end

  def oneday
    daily_snapshots_old_to_new
    @page_title = 'Open Wall, one day in life...'
    render 'snapshots/oneday'
  end

  private

  # A JPEG upload is already a JPEG. Asking ActiveStorage to represent it as
  # one ran libvips on the request thread and loaded the result into a Ruby
  # string to hand to send_data -- for a file that needed neither. Only HEIF,
  # which most browsers cannot display, has to be converted, and that is a
  # conversion ProcessImagesJob has usually done already.
  def send_camera_jpeg
    return send_blob(@snapshot.file, @snapshot.filename_for_download) if
      @snapshot.file.content_type == 'image/jpeg'

    representation = @snapshot.file.representation(format: :jpeg).processed
    send_blob(representation.image, @snapshot.filename_for_download.sub(/heif$/, 'jpg'))
  end

  # Stream the bytes rather than reading them into a string first.
  #
  # send_data @snapshot.file.download pulled the whole blob -- validated up to
  # 5 MB -- into a Ruby String on the request thread before sending a byte of
  # it. With sixteen threads per worker that is a transient peak nothing
  # accounts for, and it is one of the shapes behind the container's RSS. The
  # Disk service can say where the file is; anything else still has to
  # download it.
  def send_blob(attachment_or_blob, filename)
    blob = attachment_or_blob.try(:blob) || attachment_or_blob
    service = ActiveStorage::Blob.service

    if service.respond_to?(:path_for)
      send_file service.path_for(blob.key), disposition: 'attachment',
                filename: filename, type: blob.content_type
    else
      send_data blob.download, disposition: 'attachment', filename: filename
    end
  end

  def daily_snapshots_new_to_old
    @snapshots = Snapshot.where(mac_address: @snapshot.mac_address,
                                created_at: [1.day.ago..Time.now]).order(created_at: :desc)
  end

  def daily_snapshots_old_to_new
    @snapshots = Snapshot.where(mac_address: @snapshot.mac_address,
                                created_at: [1.day.ago..Time.now]).order(:created_at)
  end

  def find_camera
    mac_address_dec = params[:id].to_i
    mac_address = mac_address_dec.to_s(16).rjust(12, '0').reverse.gsub(/(.{2})(?=.)/, '\\1:').reverse
    @snapshot = Snapshot.where(mac_address: mac_address).order(created_at: :desc).first
    redirect_to '/open-wall', alert: "No camera with ID #{mac_address_dec} here." if @snapshot.nil?
  end

  def find_snapshot
    @snapshot = Snapshot.find(params[:id])
  rescue ActiveRecord::RecordNotFound
    redirect_to '/open-wall', alert: 'No such a shaphot here.'
  end

  def permitted_params
    params.permit(:caption, :file, :firmware, :flash_size, :hostname, :mac_address,
                  :sensor, :soc, :soc_temperature, :streamer, :uptime)
  end
end
