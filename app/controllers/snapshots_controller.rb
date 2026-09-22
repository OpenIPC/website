# frozen_string_literal: true

class SnapshotsController < ApplicationController
  skip_before_action :verify_authenticity_token
  before_action :find_snapshot, only: %i[archive oneday show download slideshow]
  before_action :find_camera, only: [:camera]

  PER_PAGE = 18

  # How many of a camera's last-24h frames the server writes into the page.
  #
  # It used to write all of them, and a camera uploading at the 15-minute
  # INTERVAL_LIMIT has 96 in a day -- so /snapshots/<id> was a 62 KB page
  # carrying 96 sibling ids and 96 image URLs, and /oneday was 104 KB carrying
  # the same 96 as full-HD slides. That made one fetch of any snapshot page
  # worth 96 more addresses to whoever fetched it, which is how the Azure
  # scraper re-armed on the opaque ids within a day of #235 changing them: it
  # never had to guess an id, every page handed it a fresh batch.
  #
  # Measured 2026-09-22 over eight hours: 7,128 fetches of the show page cost
  # 617 MB, the largest single item the site served, against 604 page views the
  # beacon could see. The fleet takes the HTML and 51 images in 9,623 requests,
  # so what it wants is this list, not the pictures.
  #
  # Twelve is a screenful on the widest grid the layout has (row-cols-xxl-6,
  # two rows). The rest arrives in a lazy turbo-frame, which is a second
  # request only a client that runs JavaScript ever makes -- and this one
  # provably does not: four beacon hits across its 272 addresses, none from the
  # Azure ranges it works from.
  STRIP_EAGER = 12

  def index
    # Every camera that has uploaded in the last day, sliced in Ruby.
    #
    # #152 asked for LIMIT and OFFSET here instead, on the premise that this
    # loads about a thousand rows. It does not: the query returns one row per
    # camera, and that has been sixteen to eighteen. Paginating in SQL was
    # measured on production and made this action WORSE -- 300ms to 600ms --
    # because the expensive part is the greatest-n-per-group join, not the row
    # count, and asking for a page means also asking for a total, which runs
    # that join a second time. Slicing eighteen rows in Ruby is free; running
    # the join twice is not.
    #
    # What did help is in Snapshot.latest_per_camera: the tile captions were
    # costing an attachment and a blob apiece, 36 of the 37 queries this made.
    #
    # If the wall ever outgrows a page by enough for the row count to matter,
    # the thing to fix first is the join -- it takes ~280ms inside a request
    # for eighteen rows, which is not explained by its EXPLAIN.
    # Normalised once, and used for both. Kaminari quietly turns "abc", "0" and
    # "-3" into page one, so passing the raw parameter through to the title
    # renders the first page under <title>Open Wall, page abc</title> -- which
    # is also the og:title a link preview shows.
    page = [params[:page].to_i, 1].max
    @snapshots = Kaminari.paginate_array(Snapshot.latest_per_camera).page(page).per(PER_PAGE)
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
      head :created, location: snapshot_path(id: @snapshot)
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
    render_snapshot_page
  end

  # The rest of the strip, for a client that ran the JavaScript to ask. Also a
  # real page on its own address, so a reader without JavaScript -- and the
  # search engines, which are welcome here in a way the scraper is not -- can
  # still reach every frame by following the link the strip carries.
  #
  # ONE representation, always with the layout. `layout: !turbo_frame_request?`
  # would give this URL two bodies, and the microcache in front of Rails keys
  # on $scheme$host|$locale_key|$uri with no Turbo-Frame header in it -- so a
  # lazy frame fetch would prime the entry with a bare fragment and the next
  # reader to follow the link printed under the strip would get an unstyled
  # orphan for the next 300 seconds. Turbo extracts the frame it wants from a
  # complete document, so the only cost of this is bytes on a request that only
  # a real reader makes.
  def archive
    @snapshots = daily_snapshots_new_to_old
    @page_title = "Open Wall, image ##{params[:id]}, all frames"
    render 'snapshots/archive'
  end

  def camera
    respond_to do |format|
      format.jpg { send_camera_jpeg }
      format.html { render_snapshot_page }
    end
  end

  def download
    send_blob(@snapshot.file, @snapshot.filename_for_download)
  end

  # The carousel is a JavaScript feature -- Bootstrap advances it, and without
  # JavaScript it is one frame with 95 more hidden behind `display: none`. So
  # writing all 96 into the page served nobody who could not run the script,
  # and served the scraper 96 full-HD URLs and 96 sibling ids. The frame below
  # carries the slideshow; this page carries the first frame and the link.
  def oneday
    @page_title = 'Open Wall, one day in life...'
  end

  # One representation, with the layout, for the reason spelled out on archive.
  def slideshow
    @snapshots = daily_snapshots_old_to_new
    @page_title = 'Open Wall, one day in life...'
    render 'snapshots/slideshow'
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
    # sub(/heif$/) missed the extension cameras actually send. HEIF files are
    # named .heic far more often than .heif -- every HEIF upload in the test
    # suite is one -- so the bytes were converted to JPEG and handed over still
    # claiming to be HEIC.
    send_blob(representation.image, @snapshot.filename_for_download.sub(/hei[cf]$/i, 'jpg'))
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

  # /snapshots/<id> and /open-wall/camera/<token> render the same page from
  # the same two lines; they differ only in how find_ put @snapshot there.
  # The first screenful of the strip, plus whether there is anything behind it.
  # One query and one extra row, rather than a second COUNT over the same
  # index: this is the most-fetched address on the site and the join behind it
  # is not free.
  def render_snapshot_page
    rows = daily_snapshots_new_to_old.limit(STRIP_EAGER + 1).to_a
    @strip_more = rows.size > STRIP_EAGER
    @snapshots = rows.first(STRIP_EAGER)
    @page_title = "Open Wall, image ##{params[:id]}"
    render 'snapshots/show'
  end

  # Both return a relation and assign nothing. They used to set @snapshots as a
  # side effect, which is why every action that called one rendered the whole
  # day whether it wanted to or not.
  def daily_snapshots_new_to_old
    Snapshot.where(mac_address: @snapshot.mac_address,
                   created_at: [1.day.ago..Time.now]).order(created_at: :desc)
  end

  def daily_snapshots_old_to_new
    Snapshot.where(mac_address: @snapshot.mac_address,
                   created_at: [1.day.ago..Time.now]).order(:created_at)
  end

  # The :id here is a camera token, not a MAC. It used to be the MAC as a
  # decimal integer, which put every uploading camera's hardware address --
  # and, in its first three octets, the manufacturer -- into a public URL.
  # See Snapshot#camera_token.
  #
  # The old numeric form is deliberately not accepted any more. Keeping it
  # would leave the addresses enumerable, which is most of what was wrong with
  # it, and dropping it costs nothing: the wall keeps two days of images, so a
  # shared link to one camera has nothing behind it by the time anyone follows
  # it.
  def find_camera
    @snapshot = Snapshot.find_by_camera_token(params[:id])
    return if @snapshot

    # No echo of what was asked for: the old message repeated the decoded
    # address back to whoever probed for it.
    redirect_to locale_path('/open-wall'), alert: 'No camera here.'
  end

  # By public_id, never by row id. Snapshot.find is left alone on purpose:
  # ActiveJob deserializes a GlobalID through it, so teaching it to refuse a
  # number would break ProcessImagesJob rather than a crawler.
  def find_snapshot
    return head :gone if params[:id].to_s.match?(/\A[0-9]+\z/)

    @snapshot = Snapshot.find_by!(public_id: params[:id])
  rescue ActiveRecord::RecordNotFound
    redirect_to locale_path('/open-wall'), alert: 'No such a shaphot here.'
  end

  def permitted_params
    params.permit(:caption, :file, :firmware, :flash_size, :hostname, :mac_address,
                  :sensor, :soc, :soc_temperature, :streamer, :uptime)
  end
end
