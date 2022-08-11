class SnapshotsController < ApplicationController
  skip_before_action :verify_authenticity_token
  before_action :find_snapshot, only: [:oneday, :show, :download]
  before_action :find_camera, only: [:camera]

  def index
    page = params[:page] || 1
    sql = "SELECT s1.* FROM snapshots s1 LEFT JOIN snapshots s2" \
      " ON (s1.mac_address = s2.mac_address AND s1.created_at < s2.created_at)" \
      " WHERE s2.id IS NULL AND s1.created_at > SUBDATE(NOW(), INTERVAL 1 DAY)" \
      " ORDER BY created_at DESC"
    @snapshots = Kaminari.paginate_array(Snapshot.find_by_sql(sql)).page(page).per(18)
    @page_title = "Open Wall, page #{page}"
    render "snapshots/index"
  end

  def create
    @snapshot = Snapshot.new
    @snapshot.ip_address = request.remote_ip
    if @snapshot.update(permitted_params)
      head :created, location: snapshot_path(@snapshot)
    else
      offset = 0
      s = Snapshot.where(mac_address: permitted_params[:mac_address])
                  .order(created_at: :desc).first
      offset = (Time.now.utc - s.created_at.utc).to_i unless s.nil?
      head :too_many_requests, retry_after: Snapshot::INTERVAL_LIMIT - offset
    end
  end

  def oneday
    one_day_worth_of_snapshots
    @page_title = "Open Wall, one day in life..."
    render "snapshots/oneday"
  end

  def show
    one_day_worth_of_snapshots
    @page_title = "Open Wall, image ##{params[:id]}"
    render "snapshots/show"
  end

  def download
    send_data @snapshot.file.download, disposition: "attachment", filename: @snapshot.filename_for_download
  end

  def camera
    respond_to do |format|
      format.jpg do
        send_data @snapshot.file.download, disposition: "inline", filename: @snapshot.filename_for_download
      end
      format.html do
        one_day_worth_of_snapshots
        @page_title = "Open Wall, image ##{params[:id]}"
        render "snapshots/show"
      end
    end
  end

  private
    def find_snapshot
      @snapshot = Snapshot.find(params[:id])
    rescue ActiveRecord::RecordNotFound
      redirect_to '/open-wall', alert: "No such a shaphot here."
    end

    def find_camera
      mac_address_dec = params[:id].to_i
      mac_address = mac_address_dec.to_s(16).rjust(12, "0").reverse.gsub(/(.{2})(?=.)/, '\\1:').reverse
      @snapshot = Snapshot.where(mac_address: mac_address).order(created_at: :desc).first
      redirect_to '/open-wall', alert: "No camera with ID #{mac_address_dec} here." if @snapshot.nil?
    end

    def one_day_worth_of_snapshots
      @snapshots = Snapshot.where(mac_address: @snapshot.mac_address, created_at: [1.day.ago..Time.now])
                           .order(:created_at)
    end

    def permitted_params
      params.permit(:file, :flash_size, :mac_address, :hostname, :soc, :sensor, :firmware, :uptime, :soc_temperature)
    end
end
